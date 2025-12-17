// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package provisionertaskbis

import (
	"context"
	"testing"

	tc "github.com/juju/tc"
)

// Test suite runner
func TestAZCoordinatorSuite(t *testing.T) {
	tc.Run(t, &AZCoordinatorSuite{})
}

// AZCoordinatorSuite contains unit tests for azCoordinator.
type AZCoordinatorSuite struct{}

func (s *AZCoordinatorSuite) TestRequestZoneWithNoZonesReturnsError(c *tc.C) {
	coord := NewAZCoordinator(nil, nil)
	ctx := context.Background()

	_, err := coord.RequestZone(ctx, ZoneRequest{MachineID: "0"})
	c.Assert(err, tc.ErrorMatches, ".*suitable availability zone.*not found.*")
}

func (s *AZCoordinatorSuite) TestRequestZoneReturnsTentativeAllocation(c *tc.C) {
	coord := NewAZCoordinator([]string{"zone-a", "zone-b"}, nil)
	ctx := context.Background()

	result, err := coord.RequestZone(ctx, ZoneRequest{MachineID: "0"})
	c.Assert(err, tc.IsNil)
	c.Assert(result.IsTentative, tc.IsTrue)
	c.Assert(result.ZoneName, tc.Not(tc.Equals), "")
}

func (s *AZCoordinatorSuite) TestRequestZoneReturnsExistingTentative(c *tc.C) {
	coord := NewAZCoordinator([]string{"zone-a"}, nil)
	ctx := context.Background()

	// First request
	result1, err := coord.RequestZone(ctx, ZoneRequest{MachineID: "0"})
	c.Assert(err, tc.IsNil)
	c.Assert(result1.ZoneName, tc.Equals, "zone-a")

	// Second request for same machine returns same allocation
	result2, err := coord.RequestZone(ctx, ZoneRequest{MachineID: "0"})
	c.Assert(err, tc.IsNil)
	c.Assert(result2.ZoneName, tc.Equals, result1.ZoneName)
}

func (s *AZCoordinatorSuite) TestRequestZoneRespectsConstraints(c *tc.C) {
	coord := NewAZCoordinator([]string{"zone-a", "zone-b", "zone-c"}, nil)
	ctx := context.Background()

	// Request with constraint - should only pick zone-b
	result, err := coord.RequestZone(ctx, ZoneRequest{
		MachineID:   "0",
		Constraints: []string{"zone-b"},
	})
	c.Assert(err, tc.IsNil)
	c.Assert(result.ZoneName, tc.Equals, "zone-b")
}

func (s *AZCoordinatorSuite) TestRequestZoneReturnsErrorIfConstraintsExcludeAllZones(c *tc.C) {
	coord := NewAZCoordinator([]string{"zone-a", "zone-b"}, nil)
	ctx := context.Background()

	_, err := coord.RequestZone(ctx, ZoneRequest{
		MachineID:   "0",
		Constraints: []string{"zone-nonexistent"},
	})
	c.Assert(err, tc.ErrorMatches, ".*suitable availability zone.*not found.*")
}

func (s *AZCoordinatorSuite) TestRequestZoneAvoidsFailedZones(c *tc.C) {
	coord := NewAZCoordinator([]string{"zone-a", "zone-b"}, nil)
	ctx := context.Background()

	// Mark zone-a as failed for machine 0
	err := coord.MarkZoneFailure(ctx, "0", "zone-a")
	c.Assert(err, tc.IsNil)

	// Request should avoid zone-a
	result, err := coord.RequestZone(ctx, ZoneRequest{MachineID: "0"})
	c.Assert(err, tc.IsNil)
	c.Assert(result.ZoneName, tc.Equals, "zone-b")
}

func (s *AZCoordinatorSuite) TestRequestZoneAvoidsExcludedZones(c *tc.C) {
	coord := NewAZCoordinator([]string{"zone-a", "zone-b"}, nil)
	ctx := context.Background()

	// Exclude zone-a for machine 0
	err := coord.ExcludeFromZones(ctx, "0", []string{"zone-a"})
	c.Assert(err, tc.IsNil)

	// Request should avoid zone-a
	result, err := coord.RequestZone(ctx, ZoneRequest{MachineID: "0"})
	c.Assert(err, tc.IsNil)
	c.Assert(result.ZoneName, tc.Equals, "zone-b")
}

func (s *AZCoordinatorSuite) TestRequestZoneDistributesInDistGroup(c *tc.C) {
	coord := NewAZCoordinator([]string{"zone-a", "zone-b"}, nil)
	ctx := context.Background()

	// Refresh with machine 1 already in zone-a
	err := coord.Refresh(ctx, RefreshRequest{
		Instances: []InstanceInfo{{ID: "i-1"}},
		Machines: []MachineInfo{
			{ID: "1", InstanceID: "i-1", Zone: "zone-a"},
		},
	})
	c.Assert(err, tc.IsNil)

	// Machine 0 in same dist group should prefer zone-b
	result, err := coord.RequestZone(ctx, ZoneRequest{
		MachineID:         "0",
		DistributionGroup: []string{"0", "1"},
	})
	c.Assert(err, tc.IsNil)
	c.Assert(result.ZoneName, tc.Equals, "zone-b")
}

func (s *AZCoordinatorSuite) TestTentativeAllocationCountsForDistribution(c *tc.C) {
	coord := NewAZCoordinator([]string{"zone-a", "zone-b"}, nil)
	ctx := context.Background()

	// Request zone for machine 0 (same dist group as 1)
	result0, err := coord.RequestZone(ctx, ZoneRequest{
		MachineID:         "0",
		DistributionGroup: []string{"0", "1"},
	})
	c.Assert(err, tc.IsNil)
	firstZone := result0.ZoneName

	// Request zone for machine 1 in same dist group
	result1, err := coord.RequestZone(ctx, ZoneRequest{
		MachineID:         "1",
		DistributionGroup: []string{"0", "1"},
	})
	c.Assert(err, tc.IsNil)

	// Should be different zones because tentative allocation for 0 was counted
	c.Assert(result1.ZoneName, tc.Not(tc.Equals), firstZone)
}

func (s *AZCoordinatorSuite) TestProvisionCompleteSuccessCommitsAllocation(c *tc.C) {
	coord := NewAZCoordinator([]string{"zone-a"}, nil)
	ctx := context.Background()

	// Request and complete provisioning
	result, err := coord.RequestZone(ctx, ZoneRequest{MachineID: "0"})
	c.Assert(err, tc.IsNil)
	c.Assert(result.IsTentative, tc.IsTrue)

	err = coord.ProvisionComplete(ctx, ProvisionResult{
		MachineID:  "0",
		InstanceID: "i-0",
		ZoneName:   "zone-a",
		Success:    true,
	})
	c.Assert(err, tc.IsNil)

	// Tentative should be cleared
	c.Assert(coord.hasTentative("0"), tc.IsFalse)
	// Machine should be in committed set
	c.Assert(coord.zoneCount("zone-a"), tc.Equals, 1)
}

func (s *AZCoordinatorSuite) TestProvisionCompleteFailureMarksZoneFailed(c *tc.C) {
	coord := NewAZCoordinator([]string{"zone-a", "zone-b"}, nil)
	ctx := context.Background()

	// Request zone
	result, err := coord.RequestZone(ctx, ZoneRequest{MachineID: "0"})
	c.Assert(err, tc.IsNil)
	failedZone := result.ZoneName

	// Report failure
	err = coord.ProvisionComplete(ctx, ProvisionResult{
		MachineID: "0",
		ZoneName:  failedZone,
		Success:   false,
	})
	c.Assert(err, tc.IsNil)

	// Zone should be marked as failed for this machine
	c.Assert(coord.isFailedInZone("0", failedZone), tc.IsTrue)
	c.Assert(coord.hasTentative("0"), tc.IsFalse)
}

func (s *AZCoordinatorSuite) TestCancelRequestReleasesTentative(c *tc.C) {
	coord := NewAZCoordinator([]string{"zone-a"}, nil)
	ctx := context.Background()

	// Request zone
	_, err := coord.RequestZone(ctx, ZoneRequest{MachineID: "0"})
	c.Assert(err, tc.IsNil)
	c.Assert(coord.hasTentative("0"), tc.IsTrue)

	// Cancel
	err = coord.CancelRequest(ctx, "0")
	c.Assert(err, tc.IsNil)
	c.Assert(coord.hasTentative("0"), tc.IsFalse)

	// Zone count should be 0
	c.Assert(coord.zoneCount("zone-a"), tc.Equals, 0)
}

func (s *AZCoordinatorSuite) TestClearZoneFailuresAllowsRetry(c *tc.C) {
	coord := NewAZCoordinator([]string{"zone-a"}, nil)
	ctx := context.Background()

	// Mark failed
	err := coord.MarkZoneFailure(ctx, "0", "zone-a")
	c.Assert(err, tc.IsNil)

	// Request should fail (only zone is failed)
	_, err = coord.RequestZone(ctx, ZoneRequest{MachineID: "0"})
	c.Assert(err, tc.ErrorMatches, ".*not found.*")

	// Clear failures
	err = coord.ClearZoneFailures(ctx, "0")
	c.Assert(err, tc.IsNil)

	// Request should succeed now
	result, err := coord.RequestZone(ctx, ZoneRequest{MachineID: "0"})
	c.Assert(err, tc.IsNil)
	c.Assert(result.ZoneName, tc.Equals, "zone-a")
}

func (s *AZCoordinatorSuite) TestRemoveMachineCleansUpAllState(c *tc.C) {
	coord := NewAZCoordinator([]string{"zone-a", "zone-b"}, nil)
	ctx := context.Background()

	// Create various state for machine 0
	_, err := coord.RequestZone(ctx, ZoneRequest{MachineID: "0"})
	c.Assert(err, tc.IsNil)
	err = coord.MarkZoneFailure(ctx, "0", "zone-b")
	c.Assert(err, tc.IsNil)
	err = coord.ExcludeFromZones(ctx, "0", []string{"zone-a"})
	c.Assert(err, tc.IsNil)

	// Verify state exists
	c.Assert(coord.hasTentative("0"), tc.IsTrue)
	c.Assert(coord.isFailedInZone("0", "zone-b"), tc.IsTrue)
	c.Assert(coord.isExcludedFromZone("0", "zone-a"), tc.IsTrue)

	// Remove machine
	err = coord.RemoveMachine(ctx, "0")
	c.Assert(err, tc.IsNil)

	// All state should be cleared
	c.Assert(coord.hasTentative("0"), tc.IsFalse)
	c.Assert(coord.isFailedInZone("0", "zone-b"), tc.IsFalse)
	c.Assert(coord.isExcludedFromZone("0", "zone-a"), tc.IsFalse)
}

func (s *AZCoordinatorSuite) TestInstanceExistsAfterRefresh(c *tc.C) {
	coord := NewAZCoordinator([]string{"zone-a"}, nil)
	ctx := context.Background()

	// Before refresh
	c.Assert(coord.InstanceExists("i-0"), tc.IsFalse)

	// Refresh with instance
	err := coord.Refresh(ctx, RefreshRequest{
		Instances: []InstanceInfo{{ID: "i-0"}},
	})
	c.Assert(err, tc.IsNil)

	// After refresh
	c.Assert(coord.InstanceExists("i-0"), tc.IsTrue)
	c.Assert(coord.InstanceExists("i-nonexistent"), tc.IsFalse)
}

func (s *AZCoordinatorSuite) TestRefreshClearsPreviousMachineState(c *tc.C) {
	coord := NewAZCoordinator([]string{"zone-a", "zone-b"}, nil)
	ctx := context.Background()

	// Initial refresh
	err := coord.Refresh(ctx, RefreshRequest{
		Instances: []InstanceInfo{{ID: "i-0"}, {ID: "i-1"}},
		Machines: []MachineInfo{
			{ID: "0", InstanceID: "i-0", Zone: "zone-a"},
			{ID: "1", InstanceID: "i-1", Zone: "zone-b"},
		},
	})
	c.Assert(err, tc.IsNil)
	c.Assert(coord.zoneCount("zone-a"), tc.Equals, 1)
	c.Assert(coord.zoneCount("zone-b"), tc.Equals, 1)

	// Second refresh with different machines
	err = coord.Refresh(ctx, RefreshRequest{
		Instances: []InstanceInfo{{ID: "i-2"}},
		Machines: []MachineInfo{
			{ID: "2", InstanceID: "i-2", Zone: "zone-a"},
		},
	})
	c.Assert(err, tc.IsNil)

	// Previous machines should be gone
	c.Assert(coord.zoneCount("zone-a"), tc.Equals, 1) // Only machine 2
	c.Assert(coord.zoneCount("zone-b"), tc.Equals, 0) // Machine 1 gone
}

func (s *AZCoordinatorSuite) TestExcludeFromZonesClearsOldExclusions(c *tc.C) {
	coord := NewAZCoordinator([]string{"zone-a", "zone-b", "zone-c"}, nil)
	ctx := context.Background()

	// Exclude from zone-a and zone-b
	err := coord.ExcludeFromZones(ctx, "0", []string{"zone-a", "zone-b"})
	c.Assert(err, tc.IsNil)
	c.Assert(coord.isExcludedFromZone("0", "zone-a"), tc.IsTrue)
	c.Assert(coord.isExcludedFromZone("0", "zone-b"), tc.IsTrue)
	c.Assert(coord.isExcludedFromZone("0", "zone-c"), tc.IsFalse)

	// Change exclusions to only zone-c
	err = coord.ExcludeFromZones(ctx, "0", []string{"zone-c"})
	c.Assert(err, tc.IsNil)
	c.Assert(coord.isExcludedFromZone("0", "zone-a"), tc.IsFalse) // Cleared
	c.Assert(coord.isExcludedFromZone("0", "zone-b"), tc.IsFalse) // Cleared
	c.Assert(coord.isExcludedFromZone("0", "zone-c"), tc.IsTrue)  // Added
}

func (s *AZCoordinatorSuite) TestMarkZoneFailureForUnknownZoneReturnsError(c *tc.C) {
	coord := NewAZCoordinator([]string{"zone-a"}, nil)
	ctx := context.Background()

	err := coord.MarkZoneFailure(ctx, "0", "zone-nonexistent")
	c.Assert(err, tc.ErrorMatches, ".*zone zone-nonexistent not found.*")
}

func (s *AZCoordinatorSuite) TestProvisionCompleteWithoutTentativeIsNoop(c *tc.C) {
	coord := NewAZCoordinator([]string{"zone-a"}, nil)
	ctx := context.Background()

	// Complete without requesting zone first
	err := coord.ProvisionComplete(ctx, ProvisionResult{
		MachineID: "0",
		Success:   true,
	})
	c.Assert(err, tc.IsNil) // Should not error
}

func (s *AZCoordinatorSuite) TestCancelRequestWithoutTentativeIsNoop(c *tc.C) {
	coord := NewAZCoordinator([]string{"zone-a"}, nil)
	ctx := context.Background()

	// Cancel without requesting zone first
	err := coord.CancelRequest(ctx, "0")
	c.Assert(err, tc.IsNil) // Should not error
}
