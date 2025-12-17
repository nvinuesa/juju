// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package provisionertaskbis

import (
	"context"
	"math/rand"
	"sort"
	"sync"

	"github.com/juju/collections/set"
	"github.com/juju/errors"
)

// azCoordinator implements AZCoordinator.
// It manages zone-aware machine placement with distribution group awareness.
//
// Thread safety: all public methods are safe for concurrent access.
// The coordinator uses a single mutex to protect all state.
//
// Zone selection algorithm:
// 1. Start with all available zones
// 2. Filter by constraints (if specified)
// 3. Remove zones where this machine has failed or is excluded
// 4. Among remaining zones, pick one with lowest machine count in the distribution group
// 5. If tied, pick randomly among the tied zones
//
// Tentative allocations:
// - When RequestZone succeeds, it creates a tentative allocation
// - Tentative allocations count toward zone machine counts for subsequent requests
// - ProvisionComplete(success) commits the allocation (removes tentative flag)
// - ProvisionComplete(failure) or CancelRequest releases the tentative allocation
type azCoordinator struct {
	mu sync.Mutex

	// Zones available for placement. Map from zone name to zone state.
	zones map[string]*zoneState

	// Running instances from last Refresh. Map from instance ID to zone name.
	instances map[string]string

	// Tentative allocations. Map from machine ID to allocated zone name.
	tentative map[string]string

	// logger for debug output
	logger Logger
}

// zoneState holds per-zone tracking information.
type zoneState struct {
	// MachineIDs is the set of machines with instances in this zone.
	MachineIDs set.Strings

	// FailedMachineIDs is the set of machines that failed to provision in this zone.
	FailedMachineIDs set.Strings

	// ExcludedMachineIDs is the set of machines that should never use this zone.
	ExcludedMachineIDs set.Strings

	// TentativeMachineIDs is the set of machines with tentative allocations here.
	TentativeMachineIDs set.Strings
}

// newZoneState creates a new empty zone state.
func newZoneState() *zoneState {
	return &zoneState{
		MachineIDs:          set.NewStrings(),
		FailedMachineIDs:    set.NewStrings(),
		ExcludedMachineIDs:  set.NewStrings(),
		TentativeMachineIDs: set.NewStrings(),
	}
}

// NewAZCoordinator creates a new AZ coordinator with the given available zones.
// If zones is empty, all RequestZone calls will fail with "no zones available".
func NewAZCoordinator(zones []string, logger Logger) *azCoordinator {
	c := &azCoordinator{
		zones:     make(map[string]*zoneState, len(zones)),
		instances: make(map[string]string),
		tentative: make(map[string]string),
		logger:    logger,
	}
	for _, z := range zones {
		c.zones[z] = newZoneState()
	}
	return c
}

// Refresh updates the coordinator's state from the current instances and machines.
// This should be called on every machine change batch.
func (c *azCoordinator) Refresh(ctx context.Context, req RefreshRequest) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Clear and rebuild instance map
	c.instances = make(map[string]string, len(req.Instances))
	for _, inst := range req.Instances {
		// Note: we don't have zone info in InstanceInfo; zone comes from MachineInfo
		c.instances[inst.ID] = ""
	}

	// Clear all zone machine sets and rebuild from current machines
	for _, zs := range c.zones {
		zs.MachineIDs = set.NewStrings()
	}

	for _, m := range req.Machines {
		if m.InstanceID == "" || m.Zone == "" {
			continue
		}
		// Update instance -> zone mapping
		if _, found := c.instances[m.InstanceID]; found {
			c.instances[m.InstanceID] = m.Zone
		}
		// Add machine to zone
		if zs, found := c.zones[m.Zone]; found {
			zs.MachineIDs.Add(m.ID)
		}
	}

	return nil
}

// RequestZone allocates a zone for the specified machine.
// The allocation is tentative until ProvisionComplete is called.
func (c *azCoordinator) RequestZone(ctx context.Context, req ZoneRequest) (ZoneAssignment, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if machine already has a tentative allocation
	if existing, found := c.tentative[req.MachineID]; found {
		return ZoneAssignment{ZoneName: existing, IsTentative: true}, nil
	}

	// Build candidate zones
	candidates := c.buildCandidates(req.MachineID, req.Constraints)
	if len(candidates) == 0 {
		return ZoneAssignment{}, errors.NotFoundf("suitable availability zone for machine %s", req.MachineID)
	}

	// Select zone with lowest count in distribution group
	selected := c.selectBestZone(candidates, req.DistributionGroup)

	// Create tentative allocation
	c.tentative[req.MachineID] = selected
	if zs, found := c.zones[selected]; found {
		zs.TentativeMachineIDs.Add(req.MachineID)
	}

	c.log(ctx, "machine %s assigned to zone %s (tentative)", req.MachineID, selected)

	return ZoneAssignment{
		ZoneName:    selected,
		IsTentative: true,
	}, nil
}

// buildCandidates returns zones that match constraints and are not failed/excluded.
func (c *azCoordinator) buildCandidates(machineID string, constraints []string) []string {
	var candidates []string

	// Use constraints as allowlist if specified
	constraintSet := set.NewStrings(constraints...)
	hasConstraints := len(constraints) > 0

	for zoneName, zs := range c.zones {
		// Check constraint match
		if hasConstraints && !constraintSet.Contains(zoneName) {
			continue
		}
		// Check not failed
		if zs.FailedMachineIDs.Contains(machineID) {
			continue
		}
		// Check not excluded
		if zs.ExcludedMachineIDs.Contains(machineID) {
			continue
		}
		candidates = append(candidates, zoneName)
	}

	return candidates
}

// selectBestZone picks a zone with lowest machine count in the distribution group.
// If tied, picks randomly.
func (c *azCoordinator) selectBestZone(candidates []string, distGroup []string) string {
	if len(candidates) == 1 {
		return candidates[0]
	}

	distGroupSet := set.NewStrings(distGroup...)

	// Count machines in each candidate zone that are in the distribution group
	type zoneCount struct {
		name  string
		count int
	}
	counts := make([]zoneCount, 0, len(candidates))

	for _, zoneName := range candidates {
		zs := c.zones[zoneName]
		count := 0

		// Count committed machines in dist group
		for _, mid := range zs.MachineIDs.Values() {
			if distGroupSet.Contains(mid) {
				count++
			}
		}
		// Count tentative machines in dist group
		for _, mid := range zs.TentativeMachineIDs.Values() {
			if distGroupSet.Contains(mid) {
				count++
			}
		}

		counts = append(counts, zoneCount{name: zoneName, count: count})
	}

	// Sort by count (ascending)
	sort.Slice(counts, func(i, j int) bool {
		return counts[i].count < counts[j].count
	})

	// Find all zones tied for lowest count
	minCount := counts[0].count
	var tied []string
	for _, zc := range counts {
		if zc.count == minCount {
			tied = append(tied, zc.name)
		} else {
			break // Sorted, so no more ties
		}
	}

	// Pick randomly among tied
	if len(tied) == 1 {
		return tied[0]
	}
	return tied[rand.Intn(len(tied))]
}

// ProvisionComplete notifies the coordinator that provisioning finished.
func (c *azCoordinator) ProvisionComplete(ctx context.Context, result ProvisionResult) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	tentativeZone, hasTentative := c.tentative[result.MachineID]
	if !hasTentative {
		// No tentative allocation; this can happen if the machine was removed
		c.log(ctx, "ProvisionComplete for %s: no tentative allocation found", result.MachineID)
		return nil
	}

	// Remove tentative allocation
	delete(c.tentative, result.MachineID)
	if zs, found := c.zones[tentativeZone]; found {
		zs.TentativeMachineIDs.Remove(result.MachineID)
	}

	if result.Success {
		// Commit: add machine to zone's committed set
		zone := result.ZoneName
		if zone == "" {
			zone = tentativeZone
		}
		if zs, found := c.zones[zone]; found {
			zs.MachineIDs.Add(result.MachineID)
		}
		// Track instance
		if result.InstanceID != "" {
			c.instances[result.InstanceID] = zone
		}
		c.log(ctx, "machine %s provisioning complete in zone %s", result.MachineID, zone)
	} else {
		// Failure: mark zone as failed for this machine
		if err := c.markZoneFailureLocked(result.MachineID, tentativeZone); err != nil {
			return err
		}
		c.log(ctx, "machine %s provisioning failed in zone %s: %v", result.MachineID, tentativeZone, result.Error)
	}

	return nil
}

// CancelRequest cancels a pending zone request.
func (c *azCoordinator) CancelRequest(ctx context.Context, machineID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	zone, found := c.tentative[machineID]
	if !found {
		// No tentative allocation to cancel
		return nil
	}

	delete(c.tentative, machineID)
	if zs, found := c.zones[zone]; found {
		zs.TentativeMachineIDs.Remove(machineID)
	}

	c.log(ctx, "machine %s zone request cancelled (was in %s)", machineID, zone)
	return nil
}

// MarkZoneFailure records that a zone failed for a specific machine.
func (c *azCoordinator) MarkZoneFailure(ctx context.Context, machineID, zoneName string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.markZoneFailureLocked(machineID, zoneName)
}

func (c *azCoordinator) markZoneFailureLocked(machineID, zoneName string) error {
	zs, found := c.zones[zoneName]
	if !found {
		return errors.NotFoundf("zone %s", zoneName)
	}
	zs.FailedMachineIDs.Add(machineID)
	return nil
}

// ClearZoneFailures clears all zone failure records for a machine.
func (c *azCoordinator) ClearZoneFailures(ctx context.Context, machineID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, zs := range c.zones {
		zs.FailedMachineIDs.Remove(machineID)
	}
	return nil
}

// ExcludeFromZones sets zones that a machine should never be placed in.
func (c *azCoordinator) ExcludeFromZones(ctx context.Context, machineID string, zones []string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Add exclusions
	for _, zoneName := range zones {
		if zs, found := c.zones[zoneName]; found {
			zs.ExcludedMachineIDs.Add(machineID)
		}
	}

	// Clear exclusions from zones not in the list
	excludeSet := set.NewStrings(zones...)
	for zoneName, zs := range c.zones {
		if !excludeSet.Contains(zoneName) {
			zs.ExcludedMachineIDs.Remove(machineID)
		}
	}

	return nil
}

// RemoveMachine removes all tracking state for a machine.
func (c *azCoordinator) RemoveMachine(ctx context.Context, machineID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Remove tentative allocation
	if zone, found := c.tentative[machineID]; found {
		delete(c.tentative, machineID)
		if zs, found := c.zones[zone]; found {
			zs.TentativeMachineIDs.Remove(machineID)
		}
	}

	// Remove from all zone sets
	for _, zs := range c.zones {
		zs.MachineIDs.Remove(machineID)
		zs.FailedMachineIDs.Remove(machineID)
		zs.ExcludedMachineIDs.Remove(machineID)
	}

	c.log(ctx, "machine %s removed from AZ coordinator", machineID)
	return nil
}

// InstanceExists checks if an instance ID is known to be running.
func (c *azCoordinator) InstanceExists(instanceID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, found := c.instances[instanceID]
	return found
}

// log logs a message if logger is available.
func (c *azCoordinator) log(ctx context.Context, format string, args ...any) {
	if c.logger != nil {
		c.logger.Debugf(ctx, format, args...)
	}
}

// ---- Test helpers (not part of public API) ----

// zoneCount returns the number of machines (committed + tentative) in a zone.
func (c *azCoordinator) zoneCount(zoneName string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	zs, found := c.zones[zoneName]
	if !found {
		return 0
	}
	return zs.MachineIDs.Size() + zs.TentativeMachineIDs.Size()
}

// hasTentative returns true if the machine has a tentative allocation.
func (c *azCoordinator) hasTentative(machineID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, found := c.tentative[machineID]
	return found
}

// isFailedInZone returns true if the machine is marked as failed in the zone.
func (c *azCoordinator) isFailedInZone(machineID, zoneName string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	zs, found := c.zones[zoneName]
	if !found {
		return false
	}
	return zs.FailedMachineIDs.Contains(machineID)
}

// isExcludedFromZone returns true if the machine is excluded from the zone.
func (c *azCoordinator) isExcludedFromZone(machineID, zoneName string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	zs, found := c.zones[zoneName]
	if !found {
		return false
	}
	return zs.ExcludedMachineIDs.Contains(machineID)
}
