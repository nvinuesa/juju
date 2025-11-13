// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package provisionertask

import (
	"fmt"

	"github.com/juju/errors"
)

// MachineState represents the state of a machine in the FSM.
type MachineState string

const (
	// StatePending represents a machine that needs to be provisioned.
	StatePending MachineState = "pending"
	
	// StateStarting represents a machine that is being started.
	StateStarting MachineState = "starting"
	
	// StateCancellingStart represents a machine whose start is being cancelled.
	StateCancellingStart MachineState = "cancelling-start"
	
	// StateAllocated represents a machine that has been allocated (started successfully).
	StateAllocated MachineState = "allocated"
	
	// StateRegistering represents a machine that is being registered.
	StateRegistering MachineState = "registering"
	
	// StateRunning represents a machine that is running.
	StateRunning MachineState = "running"
	
	// StateStopping represents a machine that is being stopped.
	StateStopping MachineState = "stopping"
	
	// StateDeleted represents a machine that has been deleted.
	StateDeleted MachineState = "deleted"
	
	// StateFailed represents a machine that has failed provisioning.
	StateFailed MachineState = "failed"
)

// TransitionTo transitions from the current state to a new state.
// This method validates state transitions according to the FSM rules.
// Valid transitions are defined by the state diagram.
func (s MachineState) TransitionTo(newState MachineState) (MachineState, error) {
	if newState == "" {
		return s, errors.New("invalid empty state transition")
	}

	// Define valid state transitions based on the FSM diagram
	validTransitions := map[MachineState][]MachineState{
		StatePending: {
			StateStarting,  // MachineAdded / ScheduleStart
			StateDeleted,   // Dead|Remove
		},
		StateStarting: {
			StateAllocated,       // StartOk
			StatePending,         // StartErr (transient) / Backoff
			StateFailed,          // StartErr (permanent)
			StateCancellingStart, // Dead|Remove
		},
		StateCancellingStart: {
			StateStopping, // StartOk / ScheduleStop
			StateDeleted,  // StartErr (any)
		},
		StateAllocated: {
			StateRegistering, // Auto / ScheduleRegister
			StateStopping,    // Dead|Remove
		},
		StateRegistering: {
			StateRunning,      // RegisterOk
			StateRegistering,  // RegisterErr (transient) / Backoff
			StateFailed,       // RegisterErr (permanent)
			StateStopping,     // Dead|Remove
		},
		StateRunning: {
			StateStopping, // Dead|Remove
		},
		StateStopping: {
			StateDeleted,  // StopOk
			StateStopping, // StopErr (transient) / Backoff
			StateFailed,   // StopErr (permanent)
		},
		StateDeleted: {}, // Terminal state
		StateFailed:  {}, // Terminal state
	}

	// Check if the transition is valid
	allowedStates, exists := validTransitions[s]
	if !exists {
		return s, fmt.Errorf("unknown current state: %s", s)
	}

	// Allow staying in the same state (no-op transition)
	if s == newState {
		return s, nil
	}

	// Check if the new state is in the list of allowed transitions
	for _, allowed := range allowedStates {
		if newState == allowed {
			return newState, nil
		}
	}

	return s, fmt.Errorf("invalid transition from %s to %s", s, newState)
}
