// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/juju/tc"

	"github.com/juju/juju/cmd/cmd/cmdtesting"
	"github.com/juju/juju/core/version"
	"github.com/juju/juju/internal/cloudconfig/instancecfg"
	"github.com/juju/juju/internal/recovery"
	coretesting "github.com/juju/juju/internal/testing"
)

type recoveryCommandSuite struct{}

func TestRecoveryCommandSuite(t *testing.T) { tc.Run(t, &recoveryCommandSuite{}) }

func (s *recoveryCommandSuite) TestRequiresProvisionedMode(c *tc.C) {
	for _, mode := range []string{"absent", "false", "malformed"} {
		dir := c.MkDir()
		if mode != "absent" {
			c.Assert(os.MkdirAll(filepath.Join(dir, "recovery"), 0700), tc.ErrorIsNil)
			c.Assert(os.WriteFile(filepath.Join(dir, recovery.ModeFile), []byte(mode), 0600), tc.ErrorIsNil)
		}
		_, err := cmdtesting.RunCommand(c, NewRecoveryCommand(), "--data-dir", dir)
		if mode == "malformed" {
			c.Check(err, tc.ErrorMatches, "invalid recovery mode")
			continue
		}
		c.Check(err, tc.ErrorMatches, "recovery-state requires the provisioned recovery mode file")
	}
}

func (s *recoveryCommandSuite) TestIncompleteImportFailsClosedAndCompletedImportRestarts(c *tc.C) {
	dir := c.MkDir()
	params := recovery.Params{ArchivePath: filepath.Join(dir, "absent-archive"), SHA256: strings.Repeat("a", 64), AgentVersion: version.Current, MachineName: "0",
		Node: instancecfg.StateInitializationParams{AgentVersion: version.Current, ControllerModelConfig: coretesting.ModelConfig(c)}}
	data, err := params.Marshal()
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(os.MkdirAll(filepath.Join(dir, "recovery"), 0700), tc.ErrorIsNil)
	c.Assert(os.WriteFile(filepath.Join(dir, recovery.ModeFile), []byte("true"), 0600), tc.ErrorIsNil)
	c.Assert(os.WriteFile(filepath.Join(dir, recovery.ParamsFile), data, 0600), tc.ErrorIsNil)
	c.Assert(recovery.BeginInitialisation(dir, params.SHA256), tc.ErrorIsNil)
	_, err = cmdtesting.RunCommand(c, NewRecoveryCommand(), "--data-dir", dir)
	c.Check(err, tc.ErrorMatches, "previous recovery import did not complete; provision a new replacement")
	c.Assert(recovery.WriteMarker(dir, recovery.InitialisedFile, params.SHA256), tc.ErrorIsNil)
	_, err = cmdtesting.RunCommand(c, NewRecoveryCommand(), "--data-dir", dir)
	c.Assert(err, tc.ErrorIsNil)
}
