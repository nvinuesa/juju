// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package provisionertaskbis

import (
	"testing"

	"github.com/juju/errors"
	tc "github.com/juju/tc"

	"github.com/juju/juju/core/life"
)

// Test suite runner
func TestTypesSuite(t *testing.T) {
	tc.Run(t, &TypesSuite{})
}

// TypesSuite contains invariant tests for types.
type TypesSuite struct{}

// TestMachineEventTypeStrings verifies all event types have string representations.
func (s *TypesSuite) TestMachineEventTypeStrings(c *tc.C) {
	c.Assert(EventLifeChanged.String(), tc.Equals, "LifeChanged")
	c.Assert(EventZoneAssigned.String(), tc.Equals, "ZoneAssigned")
	c.Assert(EventZoneRequestFailed.String(), tc.Equals, "ZoneRequestFailed")
}

// TestWorkerRequestTypeStrings verifies all request types have string representations.
func (s *TypesSuite) TestWorkerRequestTypeStrings(c *tc.C) {
	c.Assert(RequestZone.String(), tc.Equals, "RequestZone")
	c.Assert(RequestProvisionComplete.String(), tc.Equals, "RequestProvisionComplete")
	c.Assert(RequestCancelZone.String(), tc.Equals, "RequestCancelZone")
}

// TestStateStrings verifies all states have string representations.
func (s *TypesSuite) TestStateStrings(c *tc.C) {
	c.Assert(StatePending.String(), tc.Equals, "Pending")
	c.Assert(StateRequestingZone.String(), tc.Equals, "RequestingZone")
	c.Assert(StateProvisioning.String(), tc.Equals, "Provisioning")
	c.Assert(StateRunning.String(), tc.Equals, "Running")
	c.Assert(StateStopping.String(), tc.Equals, "Stopping")
	c.Assert(StateRemoving.String(), tc.Equals, "Removing")
	c.Assert(StateDone.String(), tc.Equals, "Done")
}

// TestStateIsTerminal verifies only Done is terminal.
func (s *TypesSuite) TestStateIsTerminal(c *tc.C) {
	c.Assert(StatePending.IsTerminal(), tc.IsFalse)
	c.Assert(StateRequestingZone.IsTerminal(), tc.IsFalse)
	c.Assert(StateProvisioning.IsTerminal(), tc.IsFalse)
	c.Assert(StateRunning.IsTerminal(), tc.IsFalse)
	c.Assert(StateStopping.IsTerminal(), tc.IsFalse)
	c.Assert(StateRemoving.IsTerminal(), tc.IsFalse)
	c.Assert(StateDone.IsTerminal(), tc.IsTrue)
}

// TestNewZoneRequestRequiresMachineID verifies zone request construction.
func (s *TypesSuite) TestNewZoneRequestConstruction(c *tc.C) {
	req := NewZoneRequest("machine-0", []string{"m1", "m2"}, []string{"zone-a"})
	c.Assert(req.Type, tc.Equals, RequestZone)
	c.Assert(req.MachineID, tc.Equals, "machine-0")

	payload, ok := req.Payload.(ZoneRequestPayload)
	c.Assert(ok, tc.IsTrue)
	c.Assert(payload.MachineID, tc.Equals, "machine-0")
	c.Assert(payload.DistributionGroup, tc.DeepEquals, []string{"m1", "m2"})
	c.Assert(payload.Constraints, tc.DeepEquals, []string{"zone-a"})
}

// TestNewProvisionCompleteRequestSuccess verifies success request construction.
func (s *TypesSuite) TestNewProvisionCompleteRequestSuccess(c *tc.C) {
	req := NewProvisionCompleteRequest("m0", "i-0", "zone-a", true, nil)
	c.Assert(req.Type, tc.Equals, RequestProvisionComplete)
	c.Assert(req.MachineID, tc.Equals, "m0")

	payload, ok := req.Payload.(ProvisionResultPayload)
	c.Assert(ok, tc.IsTrue)
	c.Assert(payload.MachineID, tc.Equals, "m0")
	c.Assert(payload.InstanceID, tc.Equals, "i-0")
	c.Assert(payload.ZoneName, tc.Equals, "zone-a")
	c.Assert(payload.Success, tc.IsTrue)
	c.Assert(payload.Error, tc.IsNil)
}

// TestNewProvisionCompleteRequestFailure verifies failure request construction.
func (s *TypesSuite) TestNewProvisionCompleteRequestFailure(c *tc.C) {
	testErr := errors.New("provision failed")
	req := NewProvisionCompleteRequest("m0", "", "zone-a", false, testErr)
	c.Assert(req.Type, tc.Equals, RequestProvisionComplete)
	c.Assert(req.MachineID, tc.Equals, "m0")

	payload, ok := req.Payload.(ProvisionResultPayload)
	c.Assert(ok, tc.IsTrue)
	c.Assert(payload.Success, tc.IsFalse)
	c.Assert(payload.Error, tc.Equals, testErr)
}

// TestNewCancelZoneRequestConstruction verifies cancel request construction.
func (s *TypesSuite) TestNewCancelZoneRequestConstruction(c *tc.C) {
	req := NewCancelZoneRequest("machine-0")
	c.Assert(req.Type, tc.Equals, RequestCancelZone)
	c.Assert(req.MachineID, tc.Equals, "machine-0")
	c.Assert(req.Payload, tc.IsNil)
}

// TestMachineEventZeroValue verifies zero value is usable.
func (s *TypesSuite) TestMachineEventZeroValue(c *tc.C) {
	var event MachineEvent
	// Zero value should have EventLifeChanged type (which is 0)
	c.Assert(event.Type, tc.Equals, EventLifeChanged)
	c.Assert(event.Life, tc.Equals, life.Value(""))
	c.Assert(event.Zone, tc.Equals, "")
	c.Assert(event.ZoneError, tc.IsNil)
}

// TestZoneAssignmentIsSuccess verifies AZResponse success check.
func (s *TypesSuite) TestAZResponseIsSuccess(c *tc.C) {
	success := AZResponse{
		MachineID: "m0",
		Zone:      ZoneAssignment{ZoneName: "zone-a", IsTentative: true},
		Error:     nil,
	}
	c.Assert(success.IsSuccess(), tc.IsTrue)

	failure := AZResponse{
		MachineID: "m0",
		Error:     errors.New("no zones available"),
	}
	c.Assert(failure.IsSuccess(), tc.IsFalse)
}

// TestProvisionResultZeroValue verifies zero value behavior.
func (s *TypesSuite) TestProvisionResultZeroValue(c *tc.C) {
	var result ProvisionResult
	c.Assert(result.MachineID, tc.Equals, "")
	c.Assert(result.InstanceID, tc.Equals, "")
	c.Assert(result.ZoneName, tc.Equals, "")
	c.Assert(result.Success, tc.IsFalse)
	c.Assert(result.Error, tc.IsNil)
}

// TestZoneRequestZeroValue verifies zero value behavior.
func (s *TypesSuite) TestZoneRequestZeroValue(c *tc.C) {
	var req ZoneRequest
	c.Assert(req.MachineID, tc.Equals, "")
	c.Assert(req.DistributionGroup, tc.IsNil)
	c.Assert(req.Constraints, tc.IsNil)
}

// TestMachineInfoZeroValue verifies zero value behavior.
func (s *TypesSuite) TestMachineInfoZeroValue(c *tc.C) {
	var info MachineInfo
	c.Assert(info.ID, tc.Equals, "")
	c.Assert(info.InstanceID, tc.Equals, "")
	c.Assert(info.Zone, tc.Equals, "")
}

// TestRefreshRequestZeroValue verifies zero value behavior.
func (s *TypesSuite) TestRefreshRequestZeroValue(c *tc.C) {
	var req RefreshRequest
	c.Assert(req.Instances, tc.IsNil)
	c.Assert(req.Machines, tc.IsNil)
}
