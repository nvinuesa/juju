// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package commands

import (
	"context"
	"os"
	"path/filepath"

	"github.com/juju/errors"
	"github.com/juju/tc"

	"github.com/juju/juju/api/jujuclient"
	"github.com/juju/juju/cmd/cmd/cmdtesting"
	"github.com/juju/juju/cmd/modelcmd"
	"github.com/juju/juju/domain/recovery"
)

func (s *recoverySuite) TestRecoveryCleanup(c *tc.C) {
	failure := errors.New("recovery failed")
	for _, test := range []struct {
		name            string
		args            []string
		recoveryErr     error
		resourceRemains bool
	}{
		{name: "failed recovery", recoveryErr: failure},
		{name: "retained failure", args: []string{"--keep-broken"}, recoveryErr: failure, resourceRemains: true},
		{name: "explicit cleanup", args: []string{"--keep-broken=false"}, recoveryErr: failure},
		{name: "successful recovery", resourceRemains: true},
		{name: "successful recovery with retention", args: []string{"--keep-broken"}, resourceRemains: true},
	} {
		comment := tc.Commentf("%s", test.name)
		resource := filepath.Join(c.MkDir(), "replacement")
		c.Assert(os.WriteFile(resource, []byte("imported data"), 0600), tc.ErrorIsNil)
		store := jujuclient.NewMemStore()
		store.Controllers["replacement"] = jujuclient.ControllerDetails{ControllerUUID: "recovered-uuid"}
		store.Controllers["unrelated"] = jujuclient.ControllerDetails{ControllerUUID: "unrelated-uuid"}
		command := &recoveryCommand{recoveryInfo: &recovery.ArchiveInfo{
			ControllerName: "replacement", CloudName: "cloud",
		}}
		command.SetClientStore(store)
		wrapped := modelcmd.Wrap(command, modelcmd.WrapSkipModelFlags, modelcmd.WrapSkipDefaultModel)
		args := append([]string{"backup.tar.gz", "backup.tar.gz.assert"}, test.args...)
		c.Assert(cmdtesting.InitCommand(wrapped, args), tc.ErrorIsNil)
		// The recovery timeout must not prevent the default cleanup.
		cancelled, cancel := context.WithCancel(c.Context())
		cancel()
		ctx := cmdtesting.Context(c).With(cancelled)
		err := command.cleanupFailedRecovery(ctx, func(ctx context.Context) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			return os.Remove(resource)
		}, true, test.recoveryErr)
		if test.recoveryErr == nil {
			c.Assert(err, tc.ErrorIsNil)
		} else {
			c.Check(err, tc.ErrorIs, failure, comment)
		}
		data, err := os.ReadFile(resource)
		if test.resourceRemains {
			c.Assert(err, tc.ErrorIsNil)
			c.Check(string(data), tc.Equals, "imported data", comment)
		} else {
			c.Check(err, tc.ErrorIs, os.ErrNotExist, comment)
		}
		_, registered := store.Controllers["replacement"]
		c.Check(registered, tc.Equals, test.recoveryErr == nil, comment)
		c.Check(store.Controllers["unrelated"].ControllerUUID, tc.Equals, "unrelated-uuid", comment)
	}
}

func (s *recoverySuite) TestRecoveryCleanupFailureRemovesRegistration(c *tc.C) {
	failure := errors.New("recovery failed")
	store := jujuclient.NewMemStore()
	store.Controllers["replacement"] = jujuclient.ControllerDetails{ControllerUUID: "recovered-uuid"}
	command := &recoveryCommand{recoveryInfo: &recovery.ArchiveInfo{ControllerName: "replacement"}}
	command.SetClientStore(store)
	err := command.cleanupFailedRecovery(cmdtesting.Context(c), func(context.Context) error {
		return errors.New("provider unavailable")
	}, true, failure)
	c.Check(err, tc.ErrorIs, failure)
	c.Check(err, tc.ErrorMatches, ".*provider unavailable.*recovery failed")
	_, registered := store.Controllers["replacement"]
	c.Check(registered, tc.IsFalse)
}

func (s *recoverySuite) TestRecoveryCleanupBeforeRegistration(c *tc.C) {
	store := jujuclient.NewMemStore()
	store.Controllers["source"] = jujuclient.ControllerDetails{ControllerUUID: "source-uuid"}
	command := &recoveryCommand{
		keepBroken:   true,
		recoveryInfo: &recovery.ArchiveInfo{ControllerName: "source"},
	}
	command.SetClientStore(store)
	failure := errors.New("provisioning did not start")
	err := command.cleanupFailedRecovery(cmdtesting.Context(c), nil, false, failure)
	c.Check(err, tc.ErrorIs, failure)
	c.Check(store.Controllers["source"].ControllerUUID, tc.Equals, "source-uuid")
}
