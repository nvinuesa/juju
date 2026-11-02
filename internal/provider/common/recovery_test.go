// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package common_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/juju/tc"

	"github.com/juju/juju/core/instance"
	"github.com/juju/juju/environs"
	envtesting "github.com/juju/juju/environs/testing"
	"github.com/juju/juju/internal/provider/common"
)

type recoveryProvisioningSuite struct{}

func TestRecoveryProvisioningSuite(t *testing.T) { tc.Run(t, &recoveryProvisioningSuite{}) }

type recoveryCleanupEnv struct {
	environs.Environ
	stopped []instance.Id
}

func (e *recoveryCleanupEnv) StopInstances(ctx context.Context, ids ...instance.Id) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	e.stopped = append(e.stopped, ids...)
	return nil
}

func (s *recoveryProvisioningSuite) TestCleanupAfterProvisioningFailure(c *tc.C) {
	env := &recoveryCleanupEnv{}
	ctx := envtesting.BootstrapContext(c.Context(), c)
	result, cleanup, err := common.ProvisionRecoveryMachine(ctx, env, environs.RecoveryMachineParams{MachineID: "1234567"},
		func(ctx environs.BootstrapContext, args environs.BootstrapParams) (*environs.BootstrapResult, error) {
			c.Check(args.RecoveryMachineID, tc.Equals, "1234567")
			args.InstanceStarted("replacement-instance")
			return nil, fmt.Errorf("opening controller ports failed")
		})
	c.Check(result, tc.IsNil)
	c.Check(err, tc.ErrorMatches, "opening controller ports failed")
	c.Assert(cleanup, tc.NotNil)
	c.Assert(cleanup(c.Context()), tc.ErrorIsNil)
	c.Check(env.stopped, tc.DeepEquals, []instance.Id{"replacement-instance"})
}

func (s *recoveryProvisioningSuite) TestFailureBeforeInstanceOwnsNothing(c *tc.C) {
	env := &recoveryCleanupEnv{}
	_, cleanup, err := common.ProvisionRecoveryMachine(envtesting.BootstrapContext(c.Context(), c), env,
		environs.RecoveryMachineParams{MachineID: "1234567"},
		func(environs.BootstrapContext, environs.BootstrapParams) (*environs.BootstrapResult, error) {
			return nil, fmt.Errorf("image unavailable")
		})
	c.Check(err, tc.ErrorMatches, "image unavailable")
	c.Assert(cleanup(c.Context()), tc.ErrorIsNil)
	c.Check(env.stopped, tc.HasLen, 0)
}

func (s *recoveryProvisioningSuite) TestRejectsBootstrapPhysicalIdentity(c *tc.C) {
	_, cleanup, err := common.ProvisionRecoveryMachine(envtesting.BootstrapContext(c.Context(), c), &recoveryCleanupEnv{},
		environs.RecoveryMachineParams{MachineID: "0"}, nil)
	c.Check(cleanup, tc.IsNil)
	c.Check(err, tc.ErrorMatches, "recovery requires a distinct physical machine identity")
}
