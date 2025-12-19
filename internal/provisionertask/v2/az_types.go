// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package provisionertaskbis

import (
	"context"
)

// AZCoordinator manages availability zone assignments and tracking.
// It provides the central coordination point for zone-aware provisioning.
// All interactions are designed to be non-blocking from the main loop perspective.
type AZCoordinator interface {
	// Refresh updates the coordinator's state with current instances and machines.
	// This should be called on every machine change event batch to keep the
	// coordinator's view of the world up to date.
	Refresh(ctx context.Context, req RefreshRequest) error

	// RequestZone asks for a zone assignment for a machine.
	// The returned ZoneAssignment.IsTentative will be true until ProvisionComplete
	// is called. Tentative allocations influence subsequent placements but are not
	// committed until provisioning succeeds.
	RequestZone(ctx context.Context, req ZoneRequest) (ZoneAssignment, error)

	// ProvisionComplete notifies the coordinator that provisioning finished.
	// On success, the tentative allocation is committed.
	// On failure, the tentative allocation is released and the zone is marked as
	// failed for this machine.
	ProvisionComplete(ctx context.Context, result ProvisionResult) error

	// CancelRequest cancels a pending zone request for a machine.
	// This should be called when a machine dies while in RequestingZone state.
	// It releases any tentative allocation.
	CancelRequest(ctx context.Context, machineID string) error

	// MarkZoneFailure records that a zone failed for a specific machine.
	// A failed zone for a machine will not be selected again for that machine
	// unless ClearZoneFailures is called.
	MarkZoneFailure(ctx context.Context, machineID, zoneName string) error

	// ClearZoneFailures clears all zone failure records for a machine.
	// This allows the machine to retry previously failed zones.
	ClearZoneFailures(ctx context.Context, machineID string) error

	// ExcludeFromZones sets zones that a machine should never be placed in.
	// Exclusions are applied first-class in zone selection and combine with
	// failure tracking.
	ExcludeFromZones(ctx context.Context, machineID string, zones []string) error

	// RemoveMachine removes ALL tracking state for a machine:
	// tentative allocations, committed placement, failures, and exclusions.
	RemoveMachine(ctx context.Context, machineID string) error

	// InstanceExists checks if an instance ID is known to be running
	// based on the latest Refresh snapshot.
	InstanceExists(instanceID string) bool
}

// RefreshRequest contains the data needed to refresh coordinator state.
// This is typically called at the start of processing a machine change batch.
type RefreshRequest struct {
	// Instances is the list of currently running instances from the broker.
	// Each entry contains the instance ID which is used to track instance presence.
	Instances []InstanceInfo

	// Machines is the current set of machines with their instance IDs and zones.
	// This is used to update the coordinator's view of machine-to-zone mappings.
	Machines []MachineInfo
}

// InstanceInfo contains minimal instance data needed for coordinator tracking.
type InstanceInfo struct {
	// ID is the provider-assigned instance ID.
	ID string
}

// MachineInfo contains machine data needed for AZ tracking.
type MachineInfo struct {
	// ID is the Juju machine ID (e.g., "0", "1/lxd/0").
	ID string

	// InstanceID is the provider instance ID, empty if not provisioned.
	InstanceID string

	// Zone is the availability zone where the instance is running, empty if unknown.
	Zone string
}

// ZoneRequest contains parameters for requesting a zone assignment.
type ZoneRequest struct {
	// MachineID is the ID of the machine requesting a zone.
	MachineID string

	// DistributionGroup is the list of machine IDs in the same distribution group.
	// The coordinator uses this to spread machines across zones within a group.
	DistributionGroup []string

	// Constraints are zone constraints from provisioning info.
	// Only zones matching these constraints will be considered.
	// Empty means no constraints (all zones eligible).
	Constraints []string
}

// ZoneAssignment is the result of a successful zone request.
type ZoneAssignment struct {
	// ZoneName is the name of the assigned availability zone.
	ZoneName string

	// IsTentative indicates whether this is a tentative allocation.
	// Tentative allocations become committed after ProvisionComplete(success).
	// They are released on ProvisionComplete(failure), CancelRequest, or RemoveMachine.
	IsTentative bool
}

// ProvisionResult reports the outcome of a provisioning attempt.
type ProvisionResult struct {
	// MachineID is the ID of the machine that was provisioned.
	MachineID string

	// InstanceID is the provider instance ID, set only on success.
	InstanceID string

	// ZoneName is the zone where provisioning was attempted.
	ZoneName string

	// Success indicates whether provisioning succeeded.
	Success bool

	// Error is set only if Success is false.
	Error error
}

// AZResponse is sent from the AZ Coordinator to the main loop.
// The main loop routes this to the appropriate machine worker.
type AZResponse struct {
	// MachineID identifies which machine worker should receive this response.
	MachineID string

	// Zone contains the zone assignment on success.
	Zone ZoneAssignment

	// Error is set if the zone request failed.
	Error error
}

// IsSuccess returns true if the response represents a successful zone assignment.
func (r AZResponse) IsSuccess() bool {
	return r.Error == nil
}
