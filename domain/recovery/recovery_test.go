// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package recovery_test

import (
	"github.com/juju/tc"

	"github.com/juju/juju/core/semversion"
	"github.com/juju/juju/domain/recovery"
)

func (s *recoverySuite) TestCheckAgentVersion(c *tc.C) {
	info := &recovery.ArchiveInfo{AgentVersion: semversion.MustParse("4.1.0")}
	c.Check(info.CheckAgentVersion(semversion.MustParse("4.1.0")), tc.ErrorIsNil)

	for _, recorded := range []string{"4.1.0", "4.1.0.7", "4.1-beta3.1"} {
		info.AgentVersion = semversion.MustParse(recorded)
		c.Assert(info.CheckAgentVersion(info.AgentVersion), tc.ErrorIsNil)
		for _, target := range []string{"4.0.0", "4.1.1", "4.1.0.8", "4.1-beta3"} {
			c.Check(info.CheckAgentVersion(semversion.MustParse(target)), tc.ErrorMatches, "archive agent version .* differs from replacement agent .*")
		}
	}
}

func (s *recoverySuite) TestModelFamily(c *tc.C) {
	info := &recovery.ArchiveInfo{Models: []recovery.ModelInfo{
		{UUID: "a", Name: "controller", ModelType: "iaas"},
		{UUID: "b", Name: "workload", ModelType: "iaas"},
	}}
	family, err := info.ModelFamily()
	c.Assert(err, tc.ErrorIsNil)
	c.Check(family, tc.Equals, "iaas")

	info.Models[1].ModelType = "caas"
	_, err = info.ModelFamily()
	c.Assert(err, tc.ErrorMatches,
		"source controller has mixed model types: model .workload. is caas, expected iaas")
}

// TestModelFamilyEmptyModels pins the guard against an empty inventory:
// ModelFamily is a public preflight gate and must error, not panic.
func (s *recoverySuite) TestModelFamilyEmptyModels(c *tc.C) {
	info := &recovery.ArchiveInfo{}
	_, err := info.ModelFamily()
	c.Assert(err, tc.ErrorMatches, "archive records no models")
}

func (s *recoverySuite) TestCheckProviderFamily(c *tc.C) {
	info := &recovery.ArchiveInfo{CloudType: "lxd"}
	c.Check(info.CheckProviderFamily("lxd"), tc.ErrorIsNil)

	err := info.CheckProviderFamily("ec2")
	c.Assert(err, tc.ErrorMatches,
		"archive comes from a .lxd. controller but the bootstrap cloud is .ec2.*")
}
