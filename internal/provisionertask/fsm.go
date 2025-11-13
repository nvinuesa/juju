// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package provisionertask

import (
	"github.com/juju/errors"
)

// MachineState represents the state of a machine in the FSM.
type MachineState string

const (
	// StateUnknown represents an unknown or unclassified machine state.
	StateUnknown MachineState = "unknown"
	
	// StatePending represents a machine that needs to be provisioned.
	StatePending MachineState = "pending"
	
	// StateStartingPlaceholder is reserved for future use when machines are being started.
	StateStartingPlaceholder MachineState = "starting"
	
	// StateRunningPlaceholder is reserved for future use when machines are running.
	StateRunningPlaceholder MachineState = "running"
	
	// StateDeadPlaceholder represents a machine that is dead and needs cleanup.
	StateDeadPlaceholder MachineState = "dead"
	
	// StateDeleted represents a machine that has been deleted.
	StateDeleted MachineState = "deleted"
	
	// StateFailed represents a machine that has failed provisioning.
	StateFailed MachineState = "failed"
)

// TransitionTo transitions from the current state to a new state.
// This method validates state transitions and returns the new state or an error.
// For now, it's a simple implementation that allows any transition.
func (s MachineState) TransitionTo(newState MachineState) (MachineState, error) {
	// TODO: Add state transition validation logic in future milestones
	// For scaffolding phase, allow any transition
	if newState == "" {
		return s, errors.New("invalid empty state transition")
	}
	return newState, nil
}
