// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package bootstrap

import (
	"context"
	stdtesting "testing"

	"github.com/juju/tc"

	jujucloud "github.com/juju/juju/cloud"
	"github.com/juju/juju/cmd/cmd/cmdtesting"
	"github.com/juju/juju/core/constraints"
	"github.com/juju/juju/environs"
	environscmd "github.com/juju/juju/environs/cmd"
	"github.com/juju/juju/internal/errors"
	coretesting "github.com/juju/juju/internal/testing"
)

// recoveryEnviron is a stub CAAS environ exercising the recovery substrate
// gate: only ConstraintsValidator is implemented; the embedded interface
// stays nil because the gate must fail before Bootstrap is reached.
type recoveryEnviron struct {
	environs.BootstrapEnviron
}

func (e *recoveryEnviron) ConstraintsValidator(ctx context.Context) (constraints.Validator, error) {
	return constraints.NewValidator(), nil
}

// checkableRecoveryEnviron adds the RecoverySubstrateChecker capability.
type checkableRecoveryEnviron struct {
	recoveryEnviron

	checkCalled bool
}

func (e *checkableRecoveryEnviron) CheckRecoverySubstrate(ctx context.Context, params environs.RecoverySubstrateParams) (*environs.RecoverySubstrateReport, error) {
	e.checkCalled = true
	return nil, errors.New("substrate check ran")
}

type recoveryCheckerSuite struct{}

func TestRecoveryCheckerSuite(t *stdtesting.T) {
	tc.Run(t, &recoveryCheckerSuite{})
}

func recoveryBootstrapArgs() BootstrapParams {
	return BootstrapParams{
		ControllerConfig: coretesting.FakeControllerConfig(),
		Cloud:            jujucloud.Cloud{Type: "microk8s"},
		Recovery: &RecoveryParams{
			SourcePath:          "/archive.tar.gz",
			SHA256:              "0123",
			ControllerUUID:      "ctrl-uuid",
			ControllerName:      "source-ctrl",
			ControllerModelUUID: "ctrl-model-uuid",
		},
	}
}

func (s *recoveryCheckerSuite) TestBootstrapCAASRecoveryRequiresSubstrateChecker(c *tc.C) {
	// The stub implements no RecoverySubstrateChecker: recovery mode must
	// refuse it rather than skipping the check.
	env := &recoveryEnviron{}

	ctx := environscmd.BootstrapContext(c.Context(), cmdtesting.Context(c))
	err := bootstrapCAAS(ctx, env, recoveryBootstrapArgs(), environs.BootstrapParams{})
	c.Assert(err, tc.ErrorMatches,
		`provider "microk8s" does not support recovery substrate verification; cannot recover onto it`)
}

func (s *recoveryCheckerSuite) TestBootstrapCAASRecoveryInvokesSubstrateChecker(c *tc.C) {
	env := &checkableRecoveryEnviron{}

	ctx := environscmd.BootstrapContext(c.Context(), cmdtesting.Context(c))
	err := bootstrapCAAS(ctx, env, recoveryBootstrapArgs(), environs.BootstrapParams{})
	c.Assert(err, tc.ErrorMatches, "substrate check ran")
	c.Check(env.checkCalled, tc.IsTrue)
}
