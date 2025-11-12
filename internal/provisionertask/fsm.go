// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package provisionertask

// MachineState represents the state of a machine in the FSM.
type MachineState string

const (
	// StateUnknown indicates the machine state is not yet determined or doesn't fit other categories.
	StateUnknown MachineState = "unknown"

	// StatePending indicates the machine is waiting to be provisioned.
	StatePending MachineState = "pending"

	// StateStartingPlaceholder is reserved for future use when a machine is being started.
	StateStartingPlaceholder MachineState = "starting"

	// StateRunningPlaceholder is reserved for future use when a machine is running.
	StateRunningPlaceholder MachineState = "running"

	// StateDeadPlaceholder indicates the machine is dead and should be removed.
	StateDeadPlaceholder MachineState = "dead"

	// StateDeleted is reserved for future use when a machine has been deleted.
	StateDeleted MachineState = "deleted"

	// StateFailed is reserved for future use when a machine provisioning has failed.
	StateFailed MachineState = "failed"
)

// MachineCtx holds the FSM state context for a single machine.
type MachineCtx struct {
	ID    string
	State MachineState
}

// ProviderOp represents an operation to be performed by the provider.
// This is a placeholder type for future integration.
type ProviderOp struct {
	// MachineID is the ID of the machine this operation is for.
	MachineID string
	// OpType could represent start, stop, etc. (reserved for future use)
	OpType string
}

// ProviderResult represents the result of a provider operation.
// This is a placeholder type for future integration.
type ProviderResult struct {
	// MachineID is the ID of the machine this result is for.
	MachineID string
	// Success indicates whether the operation succeeded.
	Success bool
	// Error contains any error that occurred.
	Error error
}
