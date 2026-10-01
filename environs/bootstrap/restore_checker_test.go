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

// restoreEnviron is a stub CAAS environ exercising the restore substrate
// gate: only ConstraintsValidator is implemented; the embedded interface
// stays nil because the gate must fail before Bootstrap is reached.
type restoreEnviron struct {
	environs.BootstrapEnviron
}

func (e *restoreEnviron) ConstraintsValidator(ctx context.Context) (constraints.Validator, error) {
	return constraints.NewValidator(), nil
}

// checkableRestoreEnviron adds the RestoreSubstrateChecker capability.
type checkableRestoreEnviron struct {
	restoreEnviron

	checkCalled bool
}

func (e *checkableRestoreEnviron) CheckRestoreSubstrate(ctx context.Context, params environs.RestoreSubstrateParams) error {
	e.checkCalled = true
	return errors.New("substrate check ran")
}

type restoreCheckerSuite struct{}

func TestRestoreCheckerSuite(t *stdtesting.T) {
	tc.Run(t, &restoreCheckerSuite{})
}

func restoreBootstrapArgs() BootstrapParams {
	return BootstrapParams{
		ControllerConfig: coretesting.FakeControllerConfig(),
		Cloud:            jujucloud.Cloud{Type: "microk8s"},
		Restore: &RestoreParams{
			SourcePath:          "/archive.tar.gz",
			SHA256:              "0123",
			ControllerUUID:      "ctrl-uuid",
			ControllerName:      "source-ctrl",
			ControllerModelUUID: "ctrl-model-uuid",
		},
	}
}

func (s *restoreCheckerSuite) TestBootstrapCAASRestoreRequiresSubstrateChecker(c *tc.C) {
	// The stub implements no RestoreSubstrateChecker: restore mode must
	// refuse it rather than skipping the check.
	env := &restoreEnviron{}

	ctx := environscmd.BootstrapContext(c.Context(), cmdtesting.Context(c))
	err := bootstrapCAAS(ctx, env, restoreBootstrapArgs(), environs.BootstrapParams{})
	c.Assert(err, tc.ErrorMatches,
		`provider "microk8s" does not support restore substrate verification; cannot restore onto it`)
}

func (s *restoreCheckerSuite) TestBootstrapCAASRestoreInvokesSubstrateChecker(c *tc.C) {
	env := &checkableRestoreEnviron{}

	ctx := environscmd.BootstrapContext(c.Context(), cmdtesting.Context(c))
	err := bootstrapCAAS(ctx, env, restoreBootstrapArgs(), environs.BootstrapParams{})
	c.Assert(err, tc.ErrorMatches, "substrate check ran")
	c.Check(env.checkCalled, tc.IsTrue)
}
