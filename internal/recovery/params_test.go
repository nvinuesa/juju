// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package recovery_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/juju/tc"

	"github.com/juju/juju/cloud"
	"github.com/juju/juju/controller"
	"github.com/juju/juju/core/instance"
	"github.com/juju/juju/core/semversion"
	"github.com/juju/juju/internal/cloudconfig/instancecfg"
	"github.com/juju/juju/internal/recovery"
	coretesting "github.com/juju/juju/internal/testing"
)

type paramsSuite struct{}

func TestParamsSuite(t *testing.T) { tc.Run(t, &paramsSuite{}) }

func (s *paramsSuite) TestParametersRoundTrip(c *tc.C) {
	dir := c.MkDir()
	version := semversion.MustParse("4.1.0.7")
	cfg := coretesting.ModelConfig(c)
	p := recovery.Params{ArchivePath: "/var/lib/juju/recovery/archive.tar.gz", SHA256: strings.Repeat("a", 64), AgentVersion: version, MachineName: "3", MachineNonce: "replacement",
		Node: instancecfg.StateInitializationParams{AgentVersion: version, ControllerModelConfig: cfg, ControllerConfig: controller.Config{"controller-uuid": testControllerUUID}, BootstrapMachineInstanceId: "new-instance", ControllerCloud: cloud.Cloud{Name: "lxd", Type: "lxd"}}}
	data, err := p.Marshal()
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(os.MkdirAll(filepath.Join(dir, "recovery"), 0700), tc.ErrorIsNil)
	c.Assert(os.WriteFile(filepath.Join(dir, recovery.ParamsFile), data, 0600), tc.ErrorIsNil)
	read, err := recovery.ReadParams(dir)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(read.AgentVersion, tc.Equals, version)
	c.Check(read.MachineName, tc.Equals, "3")
	c.Check(read.MachineNonce, tc.Equals, "replacement")
	c.Check(read.Node.BootstrapMachineInstanceId, tc.Equals, instance.Id("new-instance"))
	c.Check(read.Node.ControllerCloud, tc.DeepEquals, p.Node.ControllerCloud)
	c.Check(read.Node.ControllerModelConfig.AllAttrs(), tc.DeepEquals, cfg.AllAttrs())
	read.Node.AgentVersion.Build++
	data, err = read.Marshal()
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(os.WriteFile(filepath.Join(dir, recovery.ParamsFile), data, 0600), tc.ErrorIsNil)
	_, err = recovery.ReadParams(dir)
	c.Check(err, tc.ErrorMatches, "incomplete recovery parameters")
}

func (s *paramsSuite) TestModeFile(c *tc.C) {
	dir := c.MkDir()
	mode, err := recovery.IsRecovery(dir)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(mode, tc.IsFalse)
	c.Assert(os.MkdirAll(filepath.Join(dir, "recovery"), 0700), tc.ErrorIsNil)
	for _, value := range []string{"true\n", "false", "", "garbage"} {
		c.Assert(os.WriteFile(filepath.Join(dir, recovery.ModeFile), []byte(value), 0600), tc.ErrorIsNil)
		mode, err := recovery.IsRecovery(dir)
		if value == "" || value == "garbage" {
			c.Check(err, tc.ErrorMatches, "invalid recovery mode")
			continue
		}
		c.Assert(err, tc.ErrorIsNil)
		c.Check(mode, tc.Equals, strings.TrimSpace(value) == "true")
	}
	c.Assert(os.Remove(filepath.Join(dir, recovery.ModeFile)), tc.ErrorIsNil)
	c.Assert(os.Mkdir(filepath.Join(dir, recovery.ModeFile), 0700), tc.ErrorIsNil)
	_, err = recovery.IsRecovery(dir)
	c.Check(err, tc.NotNil)
}

func (s *paramsSuite) TestMarkersAreArchiveSpecific(c *tc.C) {
	dir := c.MkDir()
	sum := strings.Repeat("a", 64)
	found, err := recovery.HasMarker(dir, recovery.CompletedFile, sum)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(found, tc.IsFalse)
	c.Assert(recovery.WriteMarker(dir, recovery.CompletedFile, sum), tc.ErrorIsNil)
	found, err = recovery.HasMarker(dir, recovery.CompletedFile, sum)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(found, tc.IsTrue)
	_, err = recovery.HasMarker(dir, recovery.CompletedFile, strings.Repeat("b", 64))
	c.Check(err, tc.ErrorMatches, "recovery marker belongs to another archive")
}

func (s *paramsSuite) TestInitialisationCannotBeClaimedTwice(c *tc.C) {
	dir := c.MkDir()
	sum := strings.Repeat("a", 64)
	c.Assert(recovery.BeginInitialisation(dir, sum), tc.ErrorIsNil)
	err := recovery.BeginInitialisation(dir, sum)
	c.Check(err, tc.ErrorIs, os.ErrExist)
	found, err := recovery.HasMarker(dir, recovery.StartedFile, sum)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(found, tc.IsTrue)
}
