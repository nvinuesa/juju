// Copyright 2024 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package provisionertask

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/juju/errors"
	"github.com/juju/worker/v4/catacomb"

	"github.com/juju/juju/core/logger"
)

// MachineState represents the current state of a machine in the provisioning lifecycle
type MachineState int

const (
	// Pending - Machine needs to be provisioned
	StatePending MachineState = iota
	// Starting - Machine provisioning is in progress
	StateStarting
	// Running - Machine is successfully provisioned and running
	StateRunning
	// Stopping - Machine is being stopped/destroyed
	StateStopping
	// StopDeferred - Machine stop is deferred until current operation completes
	StateStopDeferred
	// Failed - Machine provisioning failed, may retry
	StateFailed
	// Dead - Machine has been stopped and removed
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
	// EventStartRequested - Request to start/provision a machine
	EventStartRequested MachineEvent = iota
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

// MachineStateMachine represents the state and metadata for a single machine
type MachineStateMachine struct {
	MachineID  string
	State      MachineState
	RetryCount int
	MaxRetries int
	LastError  error
	CreatedAt  time.Time
	UpdatedAt  time.Time
	InstanceID string
	StartingAt *time.Time
	StoppingAt *time.Time
}

// EventMessage represents an event with its associated machine and optional data
type EventMessage struct {
	MachineID string
	Event     MachineEvent
	Data      interface{} // Optional event-specific data
	Timestamp time.Time
}

// StateMachineProvisioner is the new state-machine based provisioner
type StateMachineProvisioner struct {
	catacomb catacomb.Catacomb
	logger   logger.Logger
	config   TaskConfig

	// Event channels - these act as input queues
	startQueue    chan EventMessage
	stopQueue     chan EventMessage
	retryQueue    chan EventMessage
	responseQueue chan EventMessage

	// State storage - single source of truth
	machines map[string]*MachineStateMachine
	mu       sync.RWMutex // Single mutex for all state

	// Worker pool for actual provisioning operations
	workers chan struct{} // Semaphore for concurrent operations
}

// StateTransition represents a valid state transition
type StateTransition struct {
	From   MachineState
	Event  MachineEvent
	To     MachineState
	Action func(context.Context, *MachineStateMachine, EventMessage) error
}

// validTransitions defines the state machine transitions
var validTransitions = []StateTransition{
	// From Pending
	{StatePending, EventStartRequested, StateStarting, (*StateMachineProvisioner).actionStartMachine},

	// From Starting
	{StateStarting, EventStartSucceeded, StateRunning, (*StateMachineProvisioner).actionMarkRunning},
	{StateStarting, EventStartFailed, StateFailed, (*StateMachineProvisioner).actionMarkFailed},
	{StateStarting, EventStopRequested, StateStopDeferred, (*StateMachineProvisioner).actionDeferStop},

	// From Running
	{StateRunning, EventStopRequested, StateStopping, (*StateMachineProvisioner).actionStopMachine},

	// From Stopping
	{StateStopping, EventStopCompleted, StateDead, (*StateMachineProvisioner).actionMarkDead},

	// From StopDeferred
	{StateStopDeferred, EventStartSucceeded, StateStopping, (*StateMachineProvisioner).actionStopMachine},
	{StateStopDeferred, EventStartFailed, StateDead, (*StateMachineProvisioner).actionMarkDead},

	// From Failed
	{StateFailed, EventRetryRequested, StatePending, (*StateMachineProvisioner).actionRetry},
	{StateFailed, EventStopRequested, StateDead, (*StateMachineProvisioner).actionMarkDead},
}

// NewStateMachineProvisioner creates a new state machine based provisioner
func NewStateMachineProvisioner(config TaskConfig) (*StateMachineProvisioner, error) {
	p := &StateMachineProvisioner{
		logger:        config.Logger,
		config:        config,
		startQueue:    make(chan EventMessage, 1000),
		stopQueue:     make(chan EventMessage, 1000),
		retryQueue:    make(chan EventMessage, 1000),
		responseQueue: make(chan EventMessage, 1000),
		machines:      make(map[string]*MachineStateMachine),
		workers:       make(chan struct{}, config.NumProvisionWorkers),
	}

	// Initialize worker semaphore
	for i := 0; i < config.NumProvisionWorkers; i++ {
		p.workers <- struct{}{}
	}

	err := catacomb.Invoke(catacomb.Plan{
		Site: &p.catacomb,
		Work: p.loop,
	})
	if err != nil {
		return nil, errors.Trace(err)
	}

	return p, nil
}

// Kill implements worker.Worker.Kill
func (p *StateMachineProvisioner) Kill() {
	p.catacomb.Kill(nil)
}

// Wait implements worker.Worker.Wait
func (p *StateMachineProvisioner) Wait() error {
	return p.catacomb.Wait()
}

// SetNumProvisionWorkers adjusts the worker pool size
func (p *StateMachineProvisioner) SetNumProvisionWorkers(count int) {
	// Drain current workers
	currentWorkers := 0
	for {
		select {
		case <-p.workers:
			currentWorkers++
		default:
			goto adjust
		}
	}

adjust:
	// Create new channel with correct size
	p.workers = make(chan struct{}, count)
	for i := 0; i < count; i++ {
		p.workers <- struct{}{}
	}

	p.logger.Infof("Adjusted provisioner worker pool size to %d", count)
}

// loop is the main event processing loop
func (p *StateMachineProvisioner) loop() error {
	ctx := p.catacomb.Context(context.Background())

	// Start watchers
	if err := p.startWatchers(ctx); err != nil {
		return errors.Trace(err)
	}

	for {
		select {
		case <-p.catacomb.Dying():
			return p.catacomb.ErrDying()

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
		}
	}
}

// processEvent handles a single event and potentially triggers state transitions
func (p *StateMachineProvisioner) processEvent(ctx context.Context, event EventMessage) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	machine, exists := p.machines[event.MachineID]
	if !exists && event.Event != EventStartRequested {
		// Only StartRequested can create new machines
		p.logger.Debugf("Ignoring event %s for unknown machine %s", event.Event, event.MachineID)
		return nil
	}

	if event.Event == EventStartRequested && !exists {
		// Create new machine state
		machine = &MachineStateMachine{
			MachineID:  event.MachineID,
			State:      StatePending,
			MaxRetries: p.config.RetryStartInstanceStrategy.RetryCount,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}
		p.machines[event.MachineID] = machine
	}

	// Find valid transition
	var transition *StateTransition
	for _, t := range validTransitions {
		if t.From == machine.State && t.Event == event.Event {
			transition = &t
			break
		}
	}

	if transition == nil {
		p.logger.Debugf("No valid transition from state %s for event %s on machine %s",
			machine.State, event.Event, event.MachineID)
		return nil
	}

	oldState := machine.State
	machine.State = transition.To
	machine.UpdatedAt = time.Now()

	p.logger.Debugf("Machine %s: %s -> %s (event: %s)",
		event.MachineID, oldState, machine.State, event.Event)

	// Execute transition action asynchronously if it might block
	if p.isBlockingAction(transition.Event) {
		go p.executeActionAsync(ctx, transition.Action, machine, event)
	} else {
		if err := transition.Action(ctx, machine, event); err != nil {
			p.logger.Errorf("Action failed for machine %s: %v", event.MachineID, err)
			return err
		}
	}

	return nil
}

// isBlockingAction returns true if the action might take significant time
func (p *StateMachineProvisioner) isBlockingAction(event MachineEvent) bool {
	switch event {
	case EventStartRequested, EventStopRequested:
		return true
	default:
		return false
	}
}

// executeActionAsync runs blocking actions in goroutines with worker pool control
func (p *StateMachineProvisioner) executeActionAsync(ctx context.Context, action func(context.Context, *MachineStateMachine, EventMessage) error, machine *MachineStateMachine, event EventMessage) {
	// Acquire worker slot
	select {
	case <-p.workers:
		defer func() { p.workers <- struct{}{} }()
	case <-ctx.Done():
		return
	}

	if err := action(ctx, machine, event); err != nil {
		p.logger.Errorf("Async action failed for machine %s: %v", machine.MachineID, err)
	}
}

// startWatchers initializes the machine and retry watchers
func (p *StateMachineProvisioner) startWatchers(ctx context.Context) error {
	// Watch for new machines
	machineWatcher, err := p.config.MachinesAPI.WatchModelMachines(ctx)
	if err != nil {
		return errors.Trace(err)
	}
	if err := p.catacomb.Add(machineWatcher); err != nil {
		return errors.Trace(err)
	}

	// Watch for retry events
	if p.config.RetryWatcher != nil {
		retryWatcher := p.config.RetryWatcher
		if err := p.catacomb.Add(retryWatcher); err != nil {
			return errors.Trace(err)
		}

		go p.watchRetries(ctx, retryWatcher)
	}

	go p.watchMachines(ctx, machineWatcher)

	return nil
}

// watchMachines processes machine change events
func (p *StateMachineProvisioner) watchMachines(ctx context.Context, watcher StringsWatcher) {
	for {
		select {
		case <-ctx.Done():
			return
		case changes, ok := <-watcher.Changes():
			if !ok {
				p.logger.Errorf("Machine watcher closed")
				return
			}
			for _, machineID := range changes {
				select {
				case p.startQueue <- EventMessage{
					MachineID: machineID,
					Event:     EventStartRequested,
					Timestamp: time.Now(),
				}:
				case <-ctx.Done():
					return
				}
			}
		}
	}
}

// watchRetries processes retry events
func (p *StateMachineProvisioner) watchRetries(ctx context.Context, watcher NotifyWatcher) {
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-watcher.Changes():
			if !ok {
				p.logger.Errorf("Retry watcher closed")
				return
			}
			// Get machines with transient errors and queue retry events
			p.queueRetryEvents(ctx)
		}
	}
}

// queueRetryEvents identifies failed machines that should be retried
func (p *StateMachineProvisioner) queueRetryEvents(ctx context.Context) {
	p.mu.RLock()
	var failedMachines []*MachineStateMachine
	for _, machine := range p.machines {
		if machine.State == StateFailed && machine.RetryCount < machine.MaxRetries {
			failedMachines = append(failedMachines, machine)
		}
	}
	p.mu.RUnlock()

	for _, machine := range failedMachines {
		select {
		case p.retryQueue <- EventMessage{
			MachineID: machine.MachineID,
			Event:     EventRetryRequested,
			Timestamp: time.Now(),
		}:
		case <-ctx.Done():
			return
		}
	}
}

// Action implementations follow...
func (p *StateMachineProvisioner) actionStartMachine(ctx context.Context, machine *MachineStateMachine, event EventMessage) error {
	now := time.Now()
	machine.StartingAt = &now

	// Actual provisioning logic would go here
	// This is a placeholder that simulates the complex provisioning logic
	// from the original doStartMachine method

	p.logger.Infof("Starting machine %s", machine.MachineID)

	// Simulate async operation result
	go func() {
		// Simulate provisioning delay
		time.Sleep(100 * time.Millisecond)

		// Send success/failure response
		response := EventMessage{
			MachineID: machine.MachineID,
			Event:     EventStartSucceeded, // or EventStartFailed
			Timestamp: time.Now(),
		}

		select {
		case p.responseQueue <- response:
		case <-ctx.Done():
		}
	}()

	return nil
}

func (p *StateMachineProvisioner) actionMarkRunning(ctx context.Context, machine *MachineStateMachine, event EventMessage) error {
	machine.StartingAt = nil
	p.logger.Infof("Machine %s is now running", machine.MachineID)
	return nil
}

func (p *StateMachineProvisioner) actionMarkFailed(ctx context.Context, machine *MachineStateMachine, event EventMessage) error {
	machine.StartingAt = nil
	machine.RetryCount++
	if data, ok := event.Data.(error); ok {
		machine.LastError = data
	}
	p.logger.Errorf("Machine %s failed to start (attempt %d/%d)",
		machine.MachineID, machine.RetryCount, machine.MaxRetries)
	return nil
}

func (p *StateMachineProvisioner) actionDeferStop(ctx context.Context, machine *MachineStateMachine, event EventMessage) error {
	p.logger.Infof("Deferring stop for machine %s (currently starting)", machine.MachineID)
	return nil
}

func (p *StateMachineProvisioner) actionStopMachine(ctx context.Context, machine *MachineStateMachine, event EventMessage) error {
	now := time.Now()
	machine.StoppingAt = &now

	p.logger.Infof("Stopping machine %s", machine.MachineID)

	// Simulate async stop operation
	go func() {
		time.Sleep(50 * time.Millisecond)

		response := EventMessage{
			MachineID: machine.MachineID,
			Event:     EventStopCompleted,
			Timestamp: time.Now(),
		}

		select {
		case p.responseQueue <- response:
		case <-ctx.Done():
		}
	}()

	return nil
}

func (p *StateMachineProvisioner) actionMarkDead(ctx context.Context, machine *MachineStateMachine, event EventMessage) error {
	machine.StoppingAt = nil
	p.logger.Infof("Machine %s is now dead", machine.MachineID)
	// In a real implementation, we might remove the machine from our map here
	// or keep it for audit/debugging purposes
	return nil
}

func (p *StateMachineProvisioner) actionRetry(ctx context.Context, machine *MachineStateMachine, event EventMessage) error {
	p.logger.Infof("Retrying machine %s (attempt %d/%d)",
		machine.MachineID, machine.RetryCount+1, machine.MaxRetries)
	return nil
}

// Helper interfaces to match the existing codebase patterns
type StringsWatcher interface {
	Changes() <-chan []string
	Kill()
	Wait() error
}

type NotifyWatcher interface {
	Changes() <-chan struct{}
	Kill()
	Wait() error
}
