// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package provisionertask

// MachineState represents the state of a machine in the FSM.
type MachineState int

const (
	// StateUnknown represents an unknown or unclassified machine state.
	StateUnknown MachineState = iota
	
	// StatePending represents a machine that needs to be provisioned.
	StatePending
	
	// StateStartingPlaceholder is reserved for future use when machines are being started.
	StateStartingPlaceholder
	
	// StateRunningPlaceholder is reserved for future use when machines are running.
	StateRunningPlaceholder
	
	// StateDeadPlaceholder represents a machine that is dead and needs cleanup.
	StateDeadPlaceholder
	
	// StateDeleted represents a machine that has been deleted.
	StateDeleted
	
	// StateFailed represents a machine that has failed provisioning.
	StateFailed
)

// MachineCtx holds the FSM state and context for a single machine.
type MachineCtx struct {
	// ID is the machine ID.
	ID string
	
	// State is the current FSM state of the machine.
	State MachineState
}

// ProviderOp represents an operation to be performed by the provider.
// This is a placeholder for future integration.
type ProviderOp struct {
	// Type indicates the operation type (e.g., "start", "stop").
	Type string
	
	// MachineID is the ID of the machine this operation applies to.
	MachineID string
}

// ProviderResult represents the result of a provider operation.
// This is a placeholder for future integration.
type ProviderResult struct {
	// MachineID is the ID of the machine this result applies to.
	MachineID string
	
	// Error holds any error that occurred during the operation.
	Error error
}
