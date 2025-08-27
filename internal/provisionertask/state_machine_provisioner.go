// Copyright 2024 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package provisionertask

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/juju/errors"
	"github.com/juju/names/v6"
	"github.com/juju/worker/v4/catacomb"

	apiprovisioner "github.com/juju/juju/api/agent/provisioner"
	"github.com/juju/juju/core/instance"
	"github.com/juju/juju/core/logger"
	"github.com/juju/juju/core/status"
	"github.com/juju/juju/core/watcher"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/environs/instances"
	"github.com/juju/juju/rpc/params"
)

// MachineState represents the current state of a machine in the provisioning lifecycle
type MachineState int

const (
	// StatePending - Machine needs to be provisioned
	StatePending MachineState = iota
	// StateStarting - Machine provisioning is in progress
	StateStarting
	// StateRunning - Machine is successfully provisioned and running
	StateRunning
	// StateStopping - Machine is being stopped/destroyed
	StateStopping
	// StateStopDeferred - Machine stop is deferred until current operation completes
	StateStopDeferred
	// StateFailed - Machine provisioning failed, may retry
	StateFailed
	// StateDead - Machine has been stopped and removed
	StateDead
)

func (s MachineState) String() string {
	switch s {
	case StatePending:
		return "Pending"
	case StateStarting:
		return "Starting"
	case StateRunning:
		return "Running"
	case StateStopping:
		return "Stopping"
	case StateStopDeferred:
		return "StopDeferred"
	case StateFailed:
		return "Failed"
	case StateDead:
		return "Dead"
	default:
		return fmt.Sprintf("Unknown(%d)", int(s))
	}
}

// MachineEvent represents events that can trigger state transitions
type MachineEvent int

const (
	// EventMachineChanged - Machine appeared in watcher changes
	EventMachineChanged MachineEvent = iota
	// EventStartRequested - Request to start/provision a machine
	EventStartRequested
	// EventStartSucceeded - Machine successfully started
	EventStartSucceeded
	// EventStartFailed - Machine start failed
	EventStartFailed
	// EventStopRequested - Request to stop a machine
	EventStopRequested
	// EventStopCompleted - Machine successfully stopped
	EventStopCompleted
	// EventRetryRequested - Request to retry a failed operation
	EventRetryRequested
)

func (e MachineEvent) String() string {
	switch e {
	case EventMachineChanged:
		return "MachineChanged"
	case EventStartRequested:
		return "StartRequested"
	case EventStartSucceeded:
		return "StartSucceeded"
	case EventStartFailed:
		return "StartFailed"
	case EventStopRequested:
		return "StopRequested"
	case EventStopCompleted:
		return "StopCompleted"
	case EventRetryRequested:
		return "RetryRequested"
	default:
		return fmt.Sprintf("Unknown(%d)", int(e))
	}
}

// MachineRecord represents the state and metadata for a single machine
type MachineRecord struct {
	MachineID       string
	State           MachineState
	RetryCount      int
	LastError       error
	CreatedAt       time.Time
	UpdatedAt       time.Time
	InstanceID      instance.Id
	StartParams     *environs.StartInstanceParams
	Machine         apiprovisioner.MachineProvisioner
	AvailZone       string
	PlacementPolicy string

	// Internal tracking
	startingWorkerActive bool
	stoppingWorkerActive bool
}

// EventMessage represents an event with its associated machine and optional data
type EventMessage struct {
	MachineID string
	Event     MachineEvent
	Data      interface{} // Optional event-specific data
	Timestamp time.Time
}

// stateMachineProvisioner is the new state-machine based provisioner implementation
type stateMachineProvisioner struct {
	catacomb catacomb.Catacomb

	// Configuration
	config TaskConfig
	logger logger.Logger

	// Event channels - these act as input queues
	machineChangesQueue chan EventMessage
	startQueue          chan EventMessage
	stopQueue           chan EventMessage
	retryQueue          chan EventMessage
	responseQueue       chan EventMessage

	// Control channels
	resizeWorkerPool chan int

	// State storage - single source of truth
	machines sync.Map // map[string]*MachineRecord - thread-safe map

	// Worker pool management
	workers chan struct{} // Semaphore for concurrent operations

	// Watchers
	machineWatcher watcher.StringsWatcher
	retryWatcher   watcher.NotifyWatcher

	// Event callback
	eventProcessedCb func(string)

	// Instance management
	instances      map[instance.Id]instances.Instance
	instancesMutex sync.RWMutex
}

// NewStateMachineProvisioner creates a new state machine based provisioner
func NewStateMachineProvisioner(config TaskConfig) (ProvisionerTask, error) {
	if config.Logger == nil {
		return nil, errors.NotValidf("missing logger")
	}

	p := &stateMachineProvisioner{
		config:              config,
		logger:              config.Logger,
		machineChangesQueue: make(chan EventMessage, 1000),
		startQueue:          make(chan EventMessage, 1000),
		stopQueue:           make(chan EventMessage, 1000),
		retryQueue:          make(chan EventMessage, 1000),
		responseQueue:       make(chan EventMessage, 1000),
		resizeWorkerPool:    make(chan int, 10),
		instances:           make(map[instance.Id]instances.Instance),
		eventProcessedCb:    config.EventProcessedCb,
	}

	// Initialize worker pool
	p.setWorkerPoolSize(config.NumProvisionWorkers)

	err := catacomb.Invoke(catacomb.Plan{
		Site: &p.catacomb,
		Work: p.loop,
	})
	if err != nil {
		return nil, errors.Trace(err)
	}

	return p, nil
}

// Kill implements ProvisionerTask.Kill
func (p *stateMachineProvisioner) Kill() {
	p.catacomb.Kill(nil)
}

// Wait implements ProvisionerTask.Wait
func (p *stateMachineProvisioner) Wait() error {
	return p.catacomb.Wait()
}

// SetNumProvisionWorkers implements ProvisionerTask.SetNumProvisionWorkers
func (p *stateMachineProvisioner) SetNumProvisionWorkers(count int) {
	select {
	case p.resizeWorkerPool <- count:
	case <-p.catacomb.Dying():
	}
}

// setWorkerPoolSize adjusts the worker pool size
func (p *stateMachineProvisioner) setWorkerPoolSize(count int) {
	if p.workers != nil {
		// Drain existing workers
		for len(p.workers) > 0 {
			<-p.workers
		}
	}

	// Create new channel with correct size
	p.workers = make(chan struct{}, count)
	for i := 0; i < count; i++ {
		p.workers <- struct{}{}
	}

	p.logger.Debugf("Set provisioner worker pool size to %d", count)
	p.notifyEventProcessed("eventTypeResizedWorkerPool")
}

// loop is the main event processing loop
func (p *stateMachineProvisioner) loop() error {
	ctx := p.scopedContext()

	// Initialize watchers
	if err := p.startWatchers(ctx); err != nil {
		return errors.Trace(err)
	}

	for {
		select {
		case <-p.catacomb.Dying():
			return p.catacomb.ErrDying()

		case event := <-p.machineChangesQueue:
			if err := p.processEvent(ctx, event); err != nil {
				p.logger.Errorf("Error processing machine change event: %v", err)
			}

		case event := <-p.startQueue:
			if err := p.processEvent(ctx, event); err != nil {
				p.logger.Errorf("Error processing start event: %v", err)
			}

		case event := <-p.stopQueue:
			if err := p.processEvent(ctx, event); err != nil {
				p.logger.Errorf("Error processing stop event: %v", err)
			}

		case event := <-p.retryQueue:
			if err := p.processEvent(ctx, event); err != nil {
				p.logger.Errorf("Error processing retry event: %v", err)
			}

		case event := <-p.responseQueue:
			if err := p.processEvent(ctx, event); err != nil {
				p.logger.Errorf("Error processing response event: %v", err)
			}

		case newSize := <-p.resizeWorkerPool:
			p.setWorkerPoolSize(newSize)
		}
	}
}

// processEvent handles a single event and potentially triggers state transitions
func (p *stateMachineProvisioner) processEvent(ctx context.Context, event EventMessage) error {
	machineRecord, exists := p.getMachine(event.MachineID)

	switch event.Event {
	case EventMachineChanged:
		return p.handleMachineChanged(ctx, event)
	case EventStartRequested:
		if !exists {
			return p.createAndStartMachine(ctx, event)
		}
		return p.handleStartRequested(ctx, machineRecord, event)
	case EventStartSucceeded:
		if exists {
			return p.handleStartSucceeded(ctx, machineRecord, event)
		}
	case EventStartFailed:
		if exists {
			return p.handleStartFailed(ctx, machineRecord, event)
		}
	case EventStopRequested:
		if exists {
			return p.handleStopRequested(ctx, machineRecord, event)
		}
	case EventStopCompleted:
		if exists {
			return p.handleStopCompleted(ctx, machineRecord, event)
		}
	case EventRetryRequested:
		if exists {
			return p.handleRetryRequested(ctx, machineRecord, event)
		}
	}

	return nil
}

// State transition handlers
func (p *stateMachineProvisioner) handleMachineChanged(ctx context.Context, event EventMessage) error {
	// Get machine from API to determine what action to take
	machines, err := p.config.MachinesAPI.Machines(ctx, names.NewMachineTag(event.MachineID))
	if err != nil {
		return errors.Annotatef(err, "getting machine %s", event.MachineID)
	}

	if len(machines) != 1 {
		p.logger.Debugf("Machine %s not found or multiple results", event.MachineID)
		return nil
	}

	machine := machines[0]
	if machine.Err != nil {
		return errors.Annotatef(machine.Err, "machine %s error", event.MachineID)
	}

	machineProvisioner := machine.Machine
	classification := p.classifyMachine(machineProvisioner)

	switch classification {
	case Pending:
		// Queue start request
		return p.enqueueEvent(p.startQueue, EventMessage{
			MachineID: event.MachineID,
			Event:     EventStartRequested,
			Data:      machineProvisioner,
			Timestamp: time.Now(),
		})
	case Dead:
		// Queue stop request
		return p.enqueueEvent(p.stopQueue, EventMessage{
			MachineID: event.MachineID,
			Event:     EventStopRequested,
			Data:      machineProvisioner,
			Timestamp: time.Now(),
		})
	default:
		// Machine is in a state we don't need to act on immediately
		p.logger.Tracef("Machine %s classified as %s, no action needed", event.MachineID, classification)
	}

	return nil
}

func (p *stateMachineProvisioner) createAndStartMachine(ctx context.Context, event EventMessage) error {
	machineProvisioner, ok := event.Data.(apiprovisioner.MachineProvisioner)
	if !ok {
		return errors.Errorf("invalid data type for machine %s", event.MachineID)
	}

	record := &MachineRecord{
		MachineID:  event.MachineID,
		State:      StatePending,
		RetryCount: 0,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
		Machine:    machineProvisioner,
	}

	p.setMachine(event.MachineID, record)
	p.logger.Debugf("Created machine record for %s", event.MachineID)

	return p.handleStartRequested(ctx, record, event)
}

func (p *stateMachineProvisioner) handleStartRequested(ctx context.Context, record *MachineRecord, event EventMessage) error {
	if !p.canTransition(record.State, event.Event, StateStarting) {
		p.logger.Debugf("Cannot start machine %s in state %s", record.MachineID, record.State)
		return nil
	}

	// Transition to Starting state
	record.State = StateStarting
	record.UpdatedAt = time.Now()
	record.startingWorkerActive = true

	if machineProvisioner, ok := event.Data.(apiprovisioner.MachineProvisioner); ok {
		record.Machine = machineProvisioner
	}

	p.setMachine(record.MachineID, record)

	p.logger.Infof("Starting machine %s", record.MachineID)

	// Start provisioning asynchronously
	go p.doStartMachine(ctx, record)

	return nil
}

func (p *stateMachineProvisioner) handleStartSucceeded(ctx context.Context, record *MachineRecord, event EventMessage) error {
	if !p.canTransition(record.State, event.Event, StateRunning) {
		// Check if we need to transition from StopDeferred to Stopping
		if p.canTransition(record.State, event.Event, StateStopping) {
			return p.transitionToStopping(ctx, record, event)
		}
		return nil
	}

	// Transition to Running state
	record.State = StateRunning
	record.UpdatedAt = time.Now()
	record.startingWorkerActive = false

	// Store instance information if provided
	if instanceInfo, ok := event.Data.(startSuccessData); ok {
		record.InstanceID = instanceInfo.InstanceID
		p.storeInstance(instanceInfo.InstanceID, instanceInfo.Instance)
	}

	p.setMachine(record.MachineID, record)
	p.logger.Infof("Machine %s successfully started", record.MachineID)

	return nil
}

func (p *stateMachineProvisioner) handleStartFailed(ctx context.Context, record *MachineRecord, event EventMessage) error {
	record.startingWorkerActive = false
	record.UpdatedAt = time.Now()

	if errData, ok := event.Data.(error); ok {
		record.LastError = errData
	}

	// Check if we were in StopDeferred state
	if p.canTransition(record.State, event.Event, StateDead) {
		record.State = StateDead
		p.setMachine(record.MachineID, record)
		p.logger.Infof("Machine %s marked as dead after failed start during stop", record.MachineID)
		return nil
	}

	if !p.canTransition(record.State, event.Event, StateFailed) {
		return nil
	}

	// Transition to Failed state
	record.State = StateFailed
	record.RetryCount++
	p.setMachine(record.MachineID, record)

	p.logger.Errorf("Machine %s failed to start (attempt %d/%d): %v",
		record.MachineID, record.RetryCount, p.config.RetryStartInstanceStrategy.RetryCount, record.LastError)

	// Set error status on machine
	if record.Machine != nil {
		if err := p.setErrorStatus(ctx, record.Machine, record.LastError); err != nil {
			p.logger.Errorf("Failed to set error status on machine %s: %v", record.MachineID, err)
		}
	}

	return nil
}

func (p *stateMachineProvisioner) handleStopRequested(ctx context.Context, record *MachineRecord, event EventMessage) error {
	// If machine is starting, defer the stop
	if record.State == StateStarting {
		if p.canTransition(record.State, event.Event, StateStopDeferred) {
			record.State = StateStopDeferred
			record.UpdatedAt = time.Now()
			record.stoppingWorkerActive = false // Will be set when actually stopping
			p.setMachine(record.MachineID, record)
			p.logger.Infof("Deferring stop for machine %s (currently starting)", record.MachineID)
			return nil
		}
	}

	// Direct transition to stopping
	if p.canTransition(record.State, event.Event, StateStopping) {
		return p.transitionToStopping(ctx, record, event)
	}

	// Direct transition to dead (for failed machines or other cases)
	if p.canTransition(record.State, event.Event, StateDead) {
		record.State = StateDead
		record.UpdatedAt = time.Now()
		record.stoppingWorkerActive = false
		p.setMachine(record.MachineID, record)
		p.logger.Infof("Machine %s marked as dead", record.MachineID)
		return nil
	}

	return nil
}

func (p *stateMachineProvisioner) transitionToStopping(ctx context.Context, record *MachineRecord, event EventMessage) error {
	record.State = StateStopping
	record.UpdatedAt = time.Now()
	record.stoppingWorkerActive = true
	p.setMachine(record.MachineID, record)

	p.logger.Infof("Stopping machine %s", record.MachineID)

	// Start stopping asynchronously
	go p.doStopMachine(ctx, record)

	return nil
}

func (p *stateMachineProvisioner) handleStopCompleted(ctx context.Context, record *MachineRecord, event EventMessage) error {
	if !p.canTransition(record.State, event.Event, StateDead) {
		return nil
	}

	record.State = StateDead
	record.UpdatedAt = time.Now()
	record.stoppingWorkerActive = false
	p.setMachine(record.MachineID, record)

	p.logger.Infof("Machine %s successfully stopped", record.MachineID)
	return nil
}

func (p *stateMachineProvisioner) handleRetryRequested(ctx context.Context, record *MachineRecord, event EventMessage) error {
	if !p.canTransition(record.State, event.Event, StatePending) {
		return nil
	}

	if record.RetryCount >= p.config.RetryStartInstanceStrategy.RetryCount {
		p.logger.Debugf("Machine %s has exhausted retries (%d/%d)",
			record.MachineID, record.RetryCount, p.config.RetryStartInstanceStrategy.RetryCount)
		return nil
	}

	record.State = StatePending
	record.UpdatedAt = time.Now()
	p.setMachine(record.MachineID, record)

	p.logger.Infof("Retrying machine %s (attempt %d/%d)",
		record.MachineID, record.RetryCount+1, p.config.RetryStartInstanceStrategy.RetryCount)

	// Queue a new start request
	return p.enqueueEvent(p.startQueue, EventMessage{
		MachineID: record.MachineID,
		Event:     EventStartRequested,
		Data:      record.Machine,
		Timestamp: time.Now(),
	})
}

// State machine validation
//
//	      ┌─────────────┐
//	      │   PENDING   │◄─── StartRequested
//	      └─────┬───────┘
//	            │ start_attempt
//	            ▼
//	      ┌─────────────┐
//	┌─────┤   STARTING  │
//	│     └─────────┬───┘
//	│               │
//	│start_         │start_
//	│failed         │succeeded
//	│               ▼
//	│     ┌─────────────┐
//	│     │   RUNNING   │
//	│     └─────┬───────┘
//	│           │ stop_requested
//	│           ▼
//	│     ┌─────────────┐      ┌──────────────┐
//	│     │  STOPPING   │─────►│ STOP_DEFERRED│
//	│     └─────┬───────┘      └─────┬────────┘
//	│           │                    │
//	│           │stop_completed      │stop_when_ready
//	│           ▼                    ▼
//	│     ┌─────────────┐      ┌─────────────┐
//	└────►│   FAILED    │      │    DEAD     │
//	      └─────┬───────┘      └─────────────┘
//	            │
//	            │retry_requested
//	            │(if retries < max)
//	            ▼
//	      ┌─────────────┐
//	      │   PENDING   │
//	      └─────────────┘
func (p *stateMachineProvisioner) canTransition(from MachineState, event MachineEvent, to MachineState) bool {
	validTransitions := map[MachineState]map[MachineEvent][]MachineState{
		StatePending: {
			EventStartRequested: {StateStarting},
		},
		StateStarting: {
			EventStartSucceeded: {StateRunning},
			EventStartFailed:    {StateFailed},
			EventStopRequested:  {StateStopDeferred},
		},
		StateRunning: {
			EventStopRequested: {StateStopping},
		},
		StateStopping: {
			EventStopCompleted: {StateDead},
		},
		StateStopDeferred: {
			EventStartSucceeded: {StateStopping},
			EventStartFailed:    {StateDead},
		},
		StateFailed: {
			EventRetryRequested: {StatePending},
			EventStopRequested:  {StateDead},
		},
	}

	if eventMap, exists := validTransitions[from]; exists {
		if toStates, exists := eventMap[event]; exists {
			for _, validTo := range toStates {
				if validTo == to {
					return true
				}
			}
		}
	}

	return false
}

// Machine classification (reuse existing logic)
func (p *stateMachineProvisioner) classifyMachine(machine apiprovisioner.MachineProvisioner) MachineClassification {
	return classifyMachine(machine)
}

// Worker management and async operations
func (p *stateMachineProvisioner) doStartMachine(ctx context.Context, record *MachineRecord) {
	// Acquire worker slot
	select {
	case <-p.workers:
		defer func() { p.workers <- struct{}{} }()
	case <-ctx.Done():
		p.sendResponse(record.MachineID, EventStartFailed, ctx.Err())
		return
	}

	result := p.startMachineWithRetry(ctx, record)

	if result.err != nil {
		p.sendResponse(record.MachineID, EventStartFailed, result.err)
	} else {
		p.sendResponse(record.MachineID, EventStartSucceeded, startSuccessData{
			InstanceID: result.instanceID,
			Instance:   result.instance,
		})
	}
}

func (p *stateMachineProvisioner) doStopMachine(ctx context.Context, record *MachineRecord) {
	// Acquire worker slot
	select {
	case <-p.workers:
		defer func() { p.workers <- struct{}{} }()
	case <-ctx.Done():
		p.sendResponse(record.MachineID, EventStopCompleted, nil)
		return
	}

	err := p.stopMachineInstance(ctx, record)
	if err != nil {
		p.logger.Errorf("Failed to stop machine %s: %v", record.MachineID, err)
		// Still consider it completed - we tried our best
	}

	p.sendResponse(record.MachineID, EventStopCompleted, nil)
}

// startMachineWithRetry implements the actual provisioning logic
type startResult struct {
	instanceID instance.Id
	instance   instances.Instance
	err        error
}

func (p *stateMachineProvisioner) startMachineWithRetry(ctx context.Context, record *MachineRecord) startResult {
	// This method contains the core provisioning logic from the original doStartMachine
	// Adapted to work with the state machine approach

	machine := record.Machine
	if machine == nil {
		return startResult{err: errors.New("machine is nil")}
	}

	// Get provisioning info
	provisioningInfo, err := p.config.MachinesAPI.ProvisioningInfo(ctx, []names.MachineTag{machine.MachineTag()})
	if err != nil {
		return startResult{err: errors.Annotate(err, "getting provisioning info")}
	}

	if len(provisioningInfo.Results) != 1 {
		return startResult{err: errors.New("expected exactly one provisioning info result")}
	}

	provInfo := provisioningInfo.Results[0]
	if provInfo.Error != nil {
		return startResult{err: errors.Annotate(provInfo.Error, "provisioning info error")}
	}

	// Setup instance config
	instanceConfig, err := p.constructInstanceConfig(ctx, machine, provInfo)
	if err != nil {
		return startResult{err: errors.Annotate(err, "constructing instance config")}
	}

	// Setup start instance params
	startInstanceParams, err := p.constructStartInstanceParams(ctx, machine, instanceConfig, provInfo)
	if err != nil {
		return startResult{err: errors.Annotate(err, "constructing start instance params")}
	}

	// Store params for potential retry
	record.StartParams = startInstanceParams

	// Attempt to start instance
	result, err := p.config.Broker.StartInstance(ctx, *startInstanceParams)
	if err != nil {
		return startResult{err: errors.Annotate(err, "starting instance")}
	}

	// Set instance info on machine
	if err := p.config.GetMachineInstanceInfoSetter(machine)(
		ctx,
		result.Instance.Id(),
		result.DisplayName,
		result.NetworkInfo.InterfaceByName("eth0").MACAddress, // simplified
		result.Hardware,
		instanceConfig.Networks,
		nil, // volumes - simplified for this example
		nil, // volume attachments - simplified
		nil, // charm profiles - simplified
	); err != nil {
		// Try to stop the instance we just started
		_ = p.config.Broker.StopInstances(ctx, result.Instance.Id())
		return startResult{err: errors.Annotate(err, "setting machine instance info")}
	}

	return startResult{
		instanceID: result.Instance.Id(),
		instance:   result.Instance,
	}
}

// stopMachineInstance implements the actual stopping logic
func (p *stateMachineProvisioner) stopMachineInstance(ctx context.Context, record *MachineRecord) error {
	if record.InstanceID == "" {
		// No instance to stop
		return nil
	}

	// Get the instance
	instance := p.getInstance(record.InstanceID)
	if instance == nil {
		p.logger.Debugf("Instance %s not found for machine %s", record.InstanceID, record.MachineID)
		return nil
	}

	// Stop the instance
	if err := p.config.Broker.StopInstances(ctx, record.InstanceID); err != nil {
		return errors.Annotatef(err, "stopping instance %s", record.InstanceID)
	}

	// Remove from our instances map
	p.removeInstance(record.InstanceID)

	return nil
}

// Helper methods for constructing instance config and start params
// These are simplified versions of the complex logic in the original file
func (p *stateMachineProvisioner) constructInstanceConfig(
	ctx context.Context,
	machine apiprovisioner.MachineProvisioner,
	provInfo *params.ProvisioningInfoResult,
) (*instances.InstanceConfig, error) {
	// This is a simplified version - the real implementation would include
	// all the complex logic from the original constructInstanceConfig method

	instanceConfig := &instances.InstanceConfig{
		MachineId:    machine.Id(),
		MachineNonce: "fake-nonce", // In real implementation, generate proper nonce
		Tools:        provInfo.Result.Tools,
		Series:       provInfo.Result.Base.Name,
		Networks:     []params.NetworkConfig{}, // Simplified
	}

	return instanceConfig, nil
}

func (p *stateMachineProvisioner) constructStartInstanceParams(
	ctx context.Context,
	machine apiprovisioner.MachineProvisioner,
	instanceConfig *instances.InstanceConfig,
	provInfo *params.ProvisioningInfoResult,
) (*environs.StartInstanceParams, error) {
	// This is a simplified version - the real implementation would include
	// all the complex logic from the original constructStartInstanceParams method

	startInstanceParams := &environs.StartInstanceParams{
		InstanceConfig:   instanceConfig,
		Tools:            provInfo.Result.Tools,
		Constraints:      provInfo.Result.Constraints,
		Placement:        provInfo.Result.Placement,
		ImageMetadata:    provInfo.Result.ImageMetadata,
		AvailabilityZone: "", // Would be determined by placement logic
	}

	return startInstanceParams, nil
}

// setErrorStatus sets error status on a machine (from original implementation)
func (p *stateMachineProvisioner) setErrorStatus(ctx context.Context, machine apiprovisioner.MachineProvisioner, err error) error {
	return machine.SetStatus(ctx, status.Error, err.Error(), nil)
}

// Watcher management
func (p *stateMachineProvisioner) startWatchers(ctx context.Context) error {
	// Start machine watcher
	machineWatcher, err := p.config.MachineWatcher(ctx)
	if err != nil {
		return errors.Trace(err)
	}
	p.machineWatcher = machineWatcher
	if err := p.catacomb.Add(machineWatcher); err != nil {
		return errors.Trace(err)
	}

	// Start retry watcher if available
	if p.config.RetryWatcher != nil {
		p.retryWatcher = p.config.RetryWatcher
		if err := p.catacomb.Add(p.retryWatcher); err != nil {
			return errors.Trace(err)
		}
		go p.watchRetries(ctx)
	}

	go p.watchMachineChanges(ctx)

	return nil
}

func (p *stateMachineProvisioner) watchMachineChanges(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case changes, ok := <-p.machineWatcher.Changes():
			if !ok {
				p.logger.Errorf("Machine watcher closed")
				return
			}

			for _, machineID := range changes {
				select {
				case p.machineChangesQueue <- EventMessage{
					MachineID: machineID,
					Event:     EventMachineChanged,
					Timestamp: time.Now(),
				}:
				case <-ctx.Done():
					return
				}
			}
			p.notifyEventProcessed("eventTypeProcessedMachines")
		}
	}
}

func (p *stateMachineProvisioner) watchRetries(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-p.retryWatcher.Changes():
			if !ok {
				p.logger.Errorf("Retry watcher closed")
				return
			}

			// Get machines with transient errors and queue retry events
			p.queueRetryEvents(ctx)
			p.notifyEventProcessed("eventTypeRetriedMachinesWithErrors")
		}
	}
}

func (p *stateMachineProvisioner) queueRetryEvents(ctx context.Context) {
	machines, err := p.config.MachinesAPI.MachinesWithTransientErrors(ctx)
	if err != nil {
		p.logger.Errorf("Failed to get machines with transient errors: %v", err)
		return
	}

	for _, machineStatus := range machines {
		if machineStatus.Err != nil {
			continue
		}

		machineID := machineStatus.Machine.Id()
		if record, exists := p.getMachine(machineID); exists {
			if record.State == StateFailed && record.RetryCount < p.config.RetryStartInstanceStrategy.RetryCount {
				select {
				case p.retryQueue <- EventMessage{
					MachineID: machineID,
					Event:     EventRetryRequested,
					Timestamp: time.Now(),
				}:
				case <-ctx.Done():
					return
				}
			}
		}
	}
}

// Utility methods
func (p *stateMachineProvisioner) scopedContext() context.Context {
	return p.catacomb.Context(context.Background())
}

func (p *stateMachineProvisioner) getMachine(machineID string) (*MachineRecord, bool) {
	if value, ok := p.machines.Load(machineID); ok {
		return value.(*MachineRecord), true
	}
	return nil, false
}

func (p *stateMachineProvisioner) setMachine(machineID string, record *MachineRecord) {
	p.machines.Store(machineID, record)
}

func (p *stateMachineProvisioner) enqueueEvent(queue chan EventMessage, event EventMessage) error {
	select {
	case queue <- event:
		return nil
	case <-p.catacomb.Dying():
		return p.catacomb.ErrDying()
	default:
		// Queue is full - this shouldn't happen in normal operation
		p.logger.Errorf("Event queue full, dropping event %s for machine %s", event.Event, event.MachineID)
		return errors.New("event queue full")
	}
}

func (p *stateMachineProvisioner) sendResponse(machineID string, event MachineEvent, data interface{}) {
	select {
	case p.responseQueue <- EventMessage{
		MachineID: machineID,
		Event:     event,
		Data:      data,
		Timestamp: time.Now(),
	}:
	case <-p.catacomb.Dying():
	}
}

// Instance management
type startSuccessData struct {
	InstanceID instance.Id
	Instance   instances.Instance
}

func (p *stateMachineProvisioner) storeInstance(id instance.Id, inst instances.Instance) {
	p.instancesMutex.Lock()
	p.instances[id] = inst
	p.instancesMutex.Unlock()
}

func (p *stateMachineProvisioner) getInstance(id instance.Id) instances.Instance {
	p.instancesMutex.RLock()
	inst := p.instances[id]
	p.instancesMutex.RUnlock()
	return inst
}

func (p *stateMachineProvisioner) removeInstance(id instance.Id) {
	p.instancesMutex.Lock()
	delete(p.instances, id)
	p.instancesMutex.Unlock()
}

func (p *stateMachineProvisioner) notifyEventProcessed(eventType string) {
	if p.eventProcessedCb != nil {
		p.eventProcessedCb(eventType)
	}
}
