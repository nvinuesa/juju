// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package provisionertaskbis

import (
	"context"
	"sync"
	"time"

	"github.com/juju/errors"
	"github.com/juju/names/v6"
	"github.com/juju/worker/v4"
	"github.com/juju/worker/v4/catacomb"

	"github.com/juju/juju/core/life"
	"github.com/juju/juju/core/status"
	"github.com/juju/juju/core/watcher"
)

// ClassifiableMachine is an interface that provides methods to classify a
// machine based on its life cycle state and instance ID.
type ClassifiableMachine interface {
	Life() life.Value
	Id() string
	InstanceId(context.Context) (string, error)
	EnsureDead(context.Context) error
	Status(context.Context) (status.Status, string, error)
	InstanceStatus(context.Context) (status.Status, string, error)
}

// MachineClassification represents the classification of a machine.
type MachineClassification string

const (
	// ClassificationNone means the machine should be ignored.
	ClassificationNone MachineClassification = "none"
	// ClassificationPending means the machine needs provisioning.
	ClassificationPending MachineClassification = "pending"
	// ClassificationDead means the machine needs cleanup.
	ClassificationDead MachineClassification = "dead"
)

// classifyMachine classifies a machine based on its life cycle state.
// This matches the legacy provisioner task classification logic.
//
// Classification rules:
// - Dying without instance -> EnsureDead, then Dead
// - Dead -> Dead
// - Alive with no instance and status Pending -> Pending
// - Alive with no instance and instance status Provisioning -> Pending
// - Everything else -> None (ignore)
func classifyMachine(ctx context.Context, logger Logger, machine ClassifiableMachine) (MachineClassification, error) {
	switch machine.Life() {
	case life.Dying:
		if _, err := machine.InstanceId(ctx); err == nil {
			// Dying with instance - ignore (will be handled when it becomes Dead)
			return ClassificationNone, nil
		} else if !isNotProvisioned(err) {
			return ClassificationNone, errors.Annotatef(err, "loading dying machine id:%s", machine.Id())
		}
		logger.Infof(ctx, "killing dying, unprovisioned machine %q", machine.Id())
		if err := machine.EnsureDead(ctx); err != nil {
			return ClassificationNone, errors.Annotatef(err, "ensuring machine dead id:%s", machine.Id())
		}
		fallthrough
	case life.Dead:
		return ClassificationDead, nil
	}

	// life.Alive
	_, err := machine.InstanceId(ctx)
	if err != nil {
		if !isNotProvisioned(err) {
			return ClassificationNone, errors.Annotatef(err, "loading machine id:%s", machine.Id())
		}
		// No instance ID
		machineStatus, _, err := machine.Status(ctx)
		if err != nil {
			logger.Infof(ctx, "cannot get machine id:%s, err:%v", machine.Id(), err)
			return ClassificationNone, nil
		}
		if machineStatus == status.Pending {
			logger.Infof(ctx, "found machine pending provisioning id:%s", machine.Id())
			return ClassificationPending, nil
		}
		instanceStatus, _, err := machine.InstanceStatus(ctx)
		if err != nil {
			logger.Infof(ctx, "cannot read instance status id:%s, err:%v", machine.Id(), err)
			return ClassificationNone, nil
		}
		if instanceStatus == status.Provisioning {
			logger.Infof(ctx, "found machine provisioning id:%s", machine.Id())
			return ClassificationPending, nil
		}
		return ClassificationNone, nil
	}
	logger.Infof(ctx, "machine %s already started", machine.Id())
	return ClassificationNone, nil
}

// isNotProvisioned returns true if the error indicates the machine is not provisioned.
// This is a placeholder - real implementation will use params.IsCodeNotProvisioned.
func isNotProvisioned(err error) bool {
	return errors.Is(err, errNotProvisioned)
}

// errNotProvisioned is used to indicate a machine has no instance.
var errNotProvisioned = errors.ConstError("not provisioned")

// ProvisionerTask interface for the bis implementation.
type ProvisionerTask interface {
	worker.Worker
	SetNumProvisionWorkers(numWorkers int)
}

// TaskConfig holds configuration for the provisioner task.
type TaskConfig struct {
	// Logger for logging.
	Logger Logger

	// MachineWatcher provides machine change notifications.
	MachineWatcher watcher.StringsWatcher

	// RetryWatcher provides retry notifications (optional).
	RetryWatcher watcher.NotifyWatcher

	// MachineGetter retrieves machines by ID.
	MachineGetter MachineGetter

	// Broker for provisioning operations.
	Broker BrokerFacade

	// InstanceInfoSetter for registering instance info.
	InstanceInfoSetter InstanceInfoSetter

	// AZCoordinator for zone-aware placement.
	AZCoordinator AZCoordinator

	// ProviderSemaphore limits concurrent provider calls.
	ProviderSemaphore *providerSemaphore

	// MaxRetries for provisioning attempts.
	MaxRetries int

	// RetryDelay between retry attempts.
	RetryDelay time.Duration

	// NumProvisionWorkers initial concurrency limit.
	NumProvisionWorkers int

	// EventProcessedCb is called after processing events (optional).
	EventProcessedCb func(string)
}

// MachineGetter retrieves machines by their IDs.
type MachineGetter interface {
	// Machines retrieves machines by their tags.
	Machines(ctx context.Context, tags ...names.MachineTag) ([]MachineResult, error)
}

// MachineResult holds a machine or an error.
type MachineResult struct {
	Machine ClassifiableMachineFull
	Err     error
}

// ClassifiableMachineFull extends ClassifiableMachine with additional operations.
type ClassifiableMachineFull interface {
	ClassifiableMachine
	// Additional methods needed by MachineWorker
	KeepInstance(ctx context.Context) (bool, error)
	SetStatus(ctx context.Context, st status.Status, msg string, data map[string]interface{}) error
	SetInstanceStatus(ctx context.Context, st status.Status, msg string, data map[string]interface{}) error
	MarkForRemoval(ctx context.Context) error
	Tag() names.MachineTag
}

// Validate validates the configuration.
func (c TaskConfig) Validate() error {
	if c.Logger == nil {
		return errors.NotValidf("nil Logger")
	}
	if c.MachineWatcher == nil {
		return errors.NotValidf("nil MachineWatcher")
	}
	if c.MachineGetter == nil {
		return errors.NotValidf("nil MachineGetter")
	}
	if c.Broker == nil {
		return errors.NotValidf("nil Broker")
	}
	if c.InstanceInfoSetter == nil {
		return errors.NotValidf("nil InstanceInfoSetter")
	}
	if c.AZCoordinator == nil {
		return errors.NotValidf("nil AZCoordinator")
	}
	if c.ProviderSemaphore == nil {
		return errors.NotValidf("nil ProviderSemaphore")
	}
	return nil
}

// workerHandle holds a machine worker and its event channel.
type workerHandle struct {
	worker     *MachineWorker
	eventsChan chan MachineEvent
}

// provisionerTask is the main orchestrator for machine provisioning.
type provisionerTask struct {
	catacomb catacomb.Catacomb
	config   TaskConfig

	// Mutex protects the workers map
	mu      sync.RWMutex
	workers map[string]*workerHandle

	// Channel for workers to send requests to the main loop
	requestChan chan WorkerRequest

	// Channel for resizing the semaphore
	resizeChan chan int
}

// Event type strings for callback.
const (
	EventProcessedMachines            = "processed-machines"
	EventRetriedMachinesWithErrors    = "retried-machines-with-errors"
	EventResizedWorkerPool            = "resized-worker-pool"
)

// NewProvisionerTask creates a new provisioner task.
func NewProvisionerTask(cfg TaskConfig) (ProvisionerTask, error) {
	if err := cfg.Validate(); err != nil {
		return nil, errors.Trace(err)
	}

	task := &provisionerTask{
		config:      cfg,
		workers:     make(map[string]*workerHandle),
		requestChan: make(chan WorkerRequest, 100), // Buffered to avoid blocking workers
		resizeChan:  make(chan int, 1),
	}

	workers := []worker.Worker{cfg.MachineWatcher}
	if cfg.RetryWatcher != nil {
		workers = append(workers, cfg.RetryWatcher)
	}

	err := catacomb.Invoke(catacomb.Plan{
		Name: "provisioner-task-bis",
		Site: &task.catacomb,
		Work: task.loop,
		Init: workers,
	})
	if err != nil {
		return nil, errors.Trace(err)
	}

	return task, nil
}

// Kill implements worker.Worker.
func (t *provisionerTask) Kill() {
	t.catacomb.Kill(nil)
}

// Wait implements worker.Worker.
func (t *provisionerTask) Wait() error {
	return t.catacomb.Wait()
}

// SetNumProvisionWorkers resizes the semaphore.
func (t *provisionerTask) SetNumProvisionWorkers(numWorkers int) {
	select {
	case t.resizeChan <- numWorkers:
	case <-t.catacomb.Dying():
	}
}

// loop is the main event loop.
func (t *provisionerTask) loop() error {
	ctx := t.catacomb.Context(context.Background())

	t.config.Logger.Infof(ctx, "entering provisioner task bis loop")
	defer t.config.Logger.Infof(ctx, "exiting provisioner task bis loop")

	machineChanges := t.config.MachineWatcher.Changes()
	var retryChanges watcher.NotifyChannel
	if t.config.RetryWatcher != nil {
		retryChanges = t.config.RetryWatcher.Changes()
	}

	for {
		select {
		case <-t.catacomb.Dying():
			return t.catacomb.ErrDying()

		case ids, ok := <-machineChanges:
			if !ok {
				return errors.New("machine watcher closed channel")
			}
			if err := t.processMachines(ctx, ids); err != nil {
				return errors.Annotate(err, "processing machines")
			}
			t.notifyCallback(EventProcessedMachines)

		case <-retryChanges:
			if err := t.processRetry(ctx); err != nil {
				return errors.Annotate(err, "processing retry")
			}
			t.notifyCallback(EventRetriedMachinesWithErrors)

		case req := <-t.requestChan:
			if err := t.handleWorkerRequest(ctx, req); err != nil {
				return errors.Annotate(err, "handling worker request")
			}

		case newSize := <-t.resizeChan:
			t.config.ProviderSemaphore.Resize(newSize)
			t.notifyCallback(EventResizedWorkerPool)
		}
	}
}

// processMachines processes a batch of machine change IDs.
func (t *provisionerTask) processMachines(ctx context.Context, ids []string) error {
	t.config.Logger.Debugf(ctx, "processing machines: %v", ids)

	// Fetch machine data
	tags := make([]names.MachineTag, len(ids))
	for i, id := range ids {
		tags[i] = names.NewMachineTag(id)
	}

	results, err := t.config.MachineGetter.Machines(ctx, tags...)
	if err != nil {
		return errors.Trace(err)
	}

	// Process each machine
	for i, result := range results {
		machineID := ids[i]

		if result.Err != nil {
			if isNotFound(result.Err) {
				// Machine no longer exists - clean up worker if any
				t.removeWorker(ctx, machineID)
				continue
			}
			t.config.Logger.Errorf(ctx, "error fetching machine %s: %v", machineID, result.Err)
			continue
		}

		machine := result.Machine
		classification, err := classifyMachine(ctx, t.config.Logger, machine)
		if err != nil {
			t.config.Logger.Errorf(ctx, "error classifying machine %s: %v", machineID, err)
			continue
		}

		t.config.Logger.Debugf(ctx, "machine %s classified as %s", machineID, classification)

		switch classification {
		case ClassificationPending:
			t.ensureWorkerAndSendEvent(ctx, machineID, machine, MachineEvent{
				Type: EventLifeChanged,
				Life: life.Alive,
			})

		case ClassificationDead:
			t.ensureWorkerAndSendEvent(ctx, machineID, machine, MachineEvent{
				Type: EventLifeChanged,
				Life: life.Dead,
			})

		case ClassificationNone:
			// Ignore - don't create or send events
			// But if worker exists and machine is running, update it
			if handle := t.getWorker(machineID); handle != nil {
				t.sendEventNonBlocking(ctx, machineID, handle, MachineEvent{
					Type: EventLifeChanged,
					Life: machine.Life(),
				})
			}
		}
	}

	return nil
}

// ensureWorkerAndSendEvent creates a worker if needed and sends an event.
func (t *provisionerTask) ensureWorkerAndSendEvent(ctx context.Context, machineID string, machine ClassifiableMachineFull, event MachineEvent) {
	handle := t.getOrCreateWorker(ctx, machineID, machine)
	if handle == nil {
		return
	}
	t.sendEventNonBlocking(ctx, machineID, handle, event)
}

// getWorker returns the worker handle for a machine, or nil if not found.
func (t *provisionerTask) getWorker(machineID string) *workerHandle {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.workers[machineID]
}

// getOrCreateWorker returns or creates a worker for the machine.
func (t *provisionerTask) getOrCreateWorker(ctx context.Context, machineID string, machine ClassifiableMachineFull) *workerHandle {
	t.mu.Lock()
	defer t.mu.Unlock()

	if handle, found := t.workers[machineID]; found {
		return handle
	}

	// Create new worker
	eventsChan := make(chan MachineEvent, 10)

	// Create machine facade adapter
	machineFacade := &machineFacadeAdapter{machine: machine}

	cfg := MachineWorkerConfig{
		MachineID:          machineID,
		Machine:            machineFacade,
		Broker:             t.config.Broker,
		InstanceInfoSetter: t.config.InstanceInfoSetter,
		Semaphore:          t.config.ProviderSemaphore,
		Logger:             t.config.Logger,
		EventsChan:         eventsChan,
		RequestChan:        t.requestChan,
		MaxRetries:         t.config.MaxRetries,
		RetryDelay:         t.config.RetryDelay,
	}

	worker, err := NewMachineWorker(cfg)
	if err != nil {
		t.config.Logger.Errorf(ctx, "failed to create worker for machine %s: %v", machineID, err)
		return nil
	}

	// Add worker to catacomb so it gets killed when task dies
	if err := t.catacomb.Add(worker); err != nil {
		t.config.Logger.Errorf(ctx, "failed to add worker to catacomb for machine %s: %v", machineID, err)
		worker.Kill()
		return nil
	}

	handle := &workerHandle{
		worker:     worker,
		eventsChan: eventsChan,
	}
	t.workers[machineID] = handle

	t.config.Logger.Debugf(ctx, "created worker for machine %s", machineID)
	return handle
}

// removeWorker stops and removes a worker.
func (t *provisionerTask) removeWorker(ctx context.Context, machineID string) {
	t.mu.Lock()
	handle, found := t.workers[machineID]
	if found {
		delete(t.workers, machineID)
	}
	t.mu.Unlock()

	if found {
		close(handle.eventsChan)
		t.config.Logger.Debugf(ctx, "removed worker for machine %s", machineID)
	}

	// Clean up AZ coordinator state
	_ = t.config.AZCoordinator.RemoveMachine(ctx, machineID)
}

// sendEventNonBlocking sends an event without blocking the main loop.
func (t *provisionerTask) sendEventNonBlocking(ctx context.Context, machineID string, handle *workerHandle, event MachineEvent) {
	select {
	case handle.eventsChan <- event:
		t.config.Logger.Tracef(ctx, "sent event %s to machine %s", event.Type, machineID)
	default:
		// Channel full - log and drop
		t.config.Logger.Warningf(ctx, "dropping event %s for machine %s - channel full", event.Type, machineID)
	}
}

// handleWorkerRequest handles requests from workers.
func (t *provisionerTask) handleWorkerRequest(ctx context.Context, req WorkerRequest) error {
	t.config.Logger.Debugf(ctx, "handling worker request: type=%s machine=%s", req.Type, req.MachineID)

	switch req.Type {
	case RequestZone:
		return t.handleZoneRequest(ctx, req)
	case RequestProvisionComplete:
		return t.handleProvisionComplete(ctx, req)
	case RequestCancelZone:
		return t.handleCancelZone(ctx, req)
	default:
		t.config.Logger.Warningf(ctx, "unknown worker request type: %d", req.Type)
	}
	return nil
}

// handleZoneRequest processes a zone assignment request.
func (t *provisionerTask) handleZoneRequest(ctx context.Context, req WorkerRequest) error {
	payload := req.Payload.(ZoneRequestPayload)

	// Request zone from coordinator
	assignment, err := t.config.AZCoordinator.RequestZone(ctx, ZoneRequest{
		MachineID:         req.MachineID,
		DistributionGroup: payload.DistributionGroup,
		Constraints:       payload.Constraints,
	})

	// Route response back to worker
	handle := t.getWorker(req.MachineID)
	if handle == nil {
		// Worker gone - cancel the zone request
		if err == nil {
			_ = t.config.AZCoordinator.CancelRequest(ctx, req.MachineID)
		}
		return nil
	}

	var event MachineEvent
	if err != nil {
		event = MachineEvent{
			Type:      EventZoneRequestFailed,
			ZoneError: err,
		}
	} else {
		event = MachineEvent{
			Type: EventZoneAssigned,
			Zone: assignment.ZoneName,
		}
	}

	t.sendEventNonBlocking(ctx, req.MachineID, handle, event)
	return nil
}

// handleProvisionComplete processes a provisioning completion notification.
func (t *provisionerTask) handleProvisionComplete(ctx context.Context, req WorkerRequest) error {
	payload := req.Payload.(ProvisionResultPayload)

	err := t.config.AZCoordinator.ProvisionComplete(ctx, ProvisionResult{
		MachineID:  payload.MachineID,
		InstanceID: payload.InstanceID,
		ZoneName:   payload.ZoneName,
		Success:    payload.Success,
		Error:      payload.Error,
	})
	if err != nil {
		t.config.Logger.Warningf(ctx, "failed to notify AZ coordinator of provision complete for %s: %v", req.MachineID, err)
	}

	// Check if worker reached Done state and remove if so
	handle := t.getWorker(req.MachineID)
	if handle != nil && handle.worker.State() == StateDone {
		t.removeWorker(ctx, req.MachineID)
	}

	return nil
}

// handleCancelZone processes a zone cancellation request.
func (t *provisionerTask) handleCancelZone(ctx context.Context, req WorkerRequest) error {
	err := t.config.AZCoordinator.CancelRequest(ctx, req.MachineID)
	if err != nil {
		t.config.Logger.Warningf(ctx, "failed to cancel zone request for %s: %v", req.MachineID, err)
	}
	return nil
}

// processRetry handles retry watcher notifications.
func (t *provisionerTask) processRetry(ctx context.Context) error {
	// In the full implementation, this would:
	// 1. Call MachinesWithTransientErrors
	// 2. Reset status to Pending for eligible machines
	// 3. Trigger re-provisioning
	//
	// For now, this is a placeholder that will be wired in Task 6.
	t.config.Logger.Debugf(ctx, "retry watcher notification (not yet wired)")
	return nil
}

// notifyCallback calls the event processed callback if set.
func (t *provisionerTask) notifyCallback(eventType string) {
	if t.config.EventProcessedCb != nil {
		t.config.EventProcessedCb(eventType)
	}
}

// isNotFound returns true if the error indicates the entity was not found.
func isNotFound(err error) bool {
	return errors.Is(err, errors.NotFound)
}

// machineFacadeAdapter adapts ClassifiableMachineFull to MachineFacade.
type machineFacadeAdapter struct {
	machine ClassifiableMachineFull
}

func (a *machineFacadeAdapter) ID() string {
	return a.machine.Id()
}

func (a *machineFacadeAdapter) Life() life.Value {
	return a.machine.Life()
}

func (a *machineFacadeAdapter) InstanceID() string {
	id, err := a.machine.InstanceId(context.Background())
	if err != nil {
		return ""
	}
	return id
}

func (a *machineFacadeAdapter) KeepInstance() bool {
	keep, _ := a.machine.KeepInstance(context.Background())
	return keep
}

func (a *machineFacadeAdapter) EnsureDead(ctx context.Context) error {
	return a.machine.EnsureDead(ctx)
}

func (a *machineFacadeAdapter) SetStatus(ctx context.Context, st status.Status, message string) error {
	return a.machine.SetStatus(ctx, st, message, nil)
}

func (a *machineFacadeAdapter) SetInstanceStatus(ctx context.Context, st status.Status, message string) error {
	return a.machine.SetInstanceStatus(ctx, st, message, nil)
}

func (a *machineFacadeAdapter) MarkForRemoval(ctx context.Context) error {
	return a.machine.MarkForRemoval(ctx)
}
