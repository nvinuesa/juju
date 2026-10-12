// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package recovery_test

import (
	"testing"

	"github.com/juju/tc"

	modeltypes "github.com/juju/juju/domain/export/types/v4_1_0"
	"github.com/juju/juju/domain/recovery"
)

type topologySuite struct{}

func TestTopologySuite(t *testing.T) { tc.Run(t, &topologySuite{}) }

func topologyDump() *recovery.ModelDump {
	return &recovery.ModelDump{
		ModelExport: modeltypes.ModelExport{
			ApplicationController: []modeltypes.ApplicationController{
				{ApplicationUUID: "controller"},
			},
			Unit: []modeltypes.Unit{
				{UUID: "unit0", Name: "controller/0", ApplicationUUID: "controller", NetNodeUUID: "node0", LifeID: 0},
				{UUID: "unit1", Name: "controller/1", ApplicationUUID: "controller", NetNodeUUID: "node1", LifeID: 0},
				{UUID: "workload", Name: "workload/0", ApplicationUUID: "workload", NetNodeUUID: "workload", LifeID: 0},
			},
			Machine: []modeltypes.Machine{
				{UUID: "machine0", Name: "0", NetNodeUUID: "node0", LifeID: 0},
				{UUID: "machine1", Name: "1", NetNodeUUID: "node1", LifeID: 0},
				{UUID: "workload", Name: "5", NetNodeUUID: "workload", LifeID: 0},
			},
			MachineCloudInstance: []modeltypes.MachineCloudInstance{
				{MachineUUID: "machine0", LifeID: 0},
			},
			ApplicationScale: []modeltypes.ApplicationScale{
				{ApplicationUUID: "controller", Scale: new(int64(2)), ScaleTarget: new(int64(3)),
					Scaling: new(true), StartOrdinal: 0},
			},
		},
	}
}

func (s *topologySuite) TestRetirePeersPreservesReplacementAndWorkloads(c *tc.C) {
	source := topologyDump()
	result, err := recovery.SingleControllerDump(source, "1")
	c.Assert(err, tc.ErrorIsNil)
	retired := result.Machine[0]
	c.Check(retired.LifeID, tc.Equals, int64(2))
	c.Check(*retired.KeepInstance, tc.Equals, true)
	c.Check(result.MachineCloudInstance[0].LifeID, tc.Equals, int64(2))
	c.Check(result.Unit[0].LifeID, tc.Equals, int64(2))
	// The replacement and the workload rows are untouched.
	c.Check(result.Unit[1], tc.DeepEquals, source.Unit[1])
	c.Check(result.Unit[2], tc.DeepEquals, source.Unit[2])
	c.Check(result.Machine[1], tc.DeepEquals, source.Machine[1])
	c.Check(result.Machine[2], tc.DeepEquals, source.Machine[2])
	c.Check(result.ApplicationScale[0], tc.DeepEquals, modeltypes.ApplicationScale{
		ApplicationUUID: "controller", Scale: new(int64(1)), ScaleTarget: new(int64(1)),
		Scaling: new(false), StartOrdinal: 1,
	})
	// The archived input is not mutated, so a failed import can be retried.
	c.Check(source, tc.DeepEquals, topologyDump())
	retry, err := recovery.SingleControllerDump(result, "1")
	c.Assert(err, tc.ErrorIsNil)
	c.Check(retry, tc.DeepEquals, result)
}

func (s *topologySuite) TestKubernetesRetiresOtherOrdinals(c *tc.C) {
	source := topologyDump()
	source.Machine = nil
	source.MachineCloudInstance = nil
	result, err := recovery.SingleControllerDump(source, "0")
	c.Assert(err, tc.ErrorIsNil)
	c.Check(result.Unit[0].LifeID, tc.Equals, int64(0))
	c.Check(result.Unit[1].LifeID, tc.Equals, int64(2))
	c.Check(result.ApplicationScale[0].StartOrdinal, tc.Equals, int64(0))
}

func (s *topologySuite) TestRejectsWorkloadsOnBothReplacedAndRetiredHosts(c *tc.C) {
	for _, node := range []string{"node0", "node1"} {
		source := topologyDump()
		source.Unit[2].NetNodeUUID = node
		_, err := recovery.SingleControllerDump(source, "1")
		c.Assert(err, tc.ErrorMatches, "controller shares a machine with workload unit.*")
	}
}

func (s *topologySuite) TestRejectsContainers(c *tc.C) {
	source := topologyDump()
	source.MachineParent = []modeltypes.MachineParent{
		{MachineUUID: "container", ParentUUID: "machine0"},
	}
	_, err := recovery.SingleControllerDump(source, "1")
	c.Assert(err, tc.ErrorMatches, "controller machine hosts containers.*")
}
