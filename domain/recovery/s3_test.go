// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package recovery_test

import (
	"github.com/juju/tc"

	"github.com/juju/juju/domain/application"
	modeltypes "github.com/juju/juju/domain/export/types/v4_1_0"
	"github.com/juju/juju/domain/recovery"
)

func (s *topologySuite) TestS3ApprovalAndHashResetTogether(c *tc.C) {
	dump := &recovery.ModelDump{
		ModelExport: modeltypes.ModelExport{
			ApplicationController: []modeltypes.ApplicationController{
				{ApplicationUUID: "controller"},
			},
			ApplicationConfig: []modeltypes.ApplicationConfig{
				{ApplicationUUID: "controller", Key: "authorize-s3-transition", Value: new("true")},
				{ApplicationUUID: "controller", Key: "nullable"},
				{ApplicationUUID: "workload", Key: "authorize-s3-transition", Value: new("true")},
			},
			ApplicationSetting: []modeltypes.ApplicationSetting{
				{ApplicationUUID: "controller", Trust: new(true)},
			},
			ApplicationConfigHash: []modeltypes.ApplicationConfigHash{
				{ApplicationUUID: "controller", Sha256: "source-controller"},
				{ApplicationUUID: "workload", Sha256: "source-workload"},
			},
		},
	}
	result := recovery.DiscardS3Authorisation(dump)
	c.Check(*result.ApplicationConfig[0].Value, tc.Equals, "false")
	c.Check(*result.ApplicationConfig[2].Value, tc.Equals, "true")
	c.Check(result.ApplicationConfigHash[0].Sha256, tc.Equals,
		application.ConfigHash(map[string]string{"authorize-s3-transition": "false", "nullable": ""}, true))
	c.Check(result.ApplicationConfigHash[1].Sha256, tc.Equals, "source-workload")
	// The archived input is not mutated, so a failed import can be retried.
	c.Check(*dump.ApplicationConfig[0].Value, tc.Equals, "true")
	c.Check(dump.ApplicationConfigHash[0].Sha256, tc.Equals, "source-controller")
	c.Check(recovery.DiscardS3Authorisation(result), tc.DeepEquals, result)
}
