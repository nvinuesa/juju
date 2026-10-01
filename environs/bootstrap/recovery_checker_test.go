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

type preparableRecoveryEnviron struct {
	recoveryEnviron
	standardCalled bool
	params         environs.RecoverySubstrateParams
	report         *environs.RecoverySubstrateReport
	err            error
}

func (e *preparableRecoveryEnviron) PrepareForBootstrap(environs.BootstrapContext, string) error {
	e.standardCalled = true
	return errors.New("normal preparation refuses surviving models")
}

func (e *preparableRecoveryEnviron) PrepareForRecovery(_ environs.BootstrapContext, params environs.RecoverySubstrateParams) (*environs.RecoverySubstrateReport, error) {
	e.params = params
	return e.report, e.err
}

func (s *recoveryCheckerSuite) TestRecoveryPreparationPreservesWorkloadNamespaces(c *tc.C) {
	report := &environs.RecoverySubstrateReport{MissingWorkloads: []string{"workload/app"}}
	env := &preparableRecoveryEnviron{report: report}
	args := recoveryBootstrapArgs()
	ctx := environscmd.BootstrapContext(c.Context(), cmdtesting.Context(c))
	err := prepareControllerEnvironment(ctx, env, PrepareParams{Recovery: args.Recovery}, true)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(env.standardCalled, tc.IsFalse)
	c.Check(env.params.ControllerUUID, tc.Equals, args.Recovery.ControllerUUID)
	c.Check(env.params.ControllerName, tc.Equals, args.Recovery.ControllerName)
	c.Check(env.params.ControllerModelUUID, tc.Equals, args.Recovery.ControllerModelUUID)
	c.Check(args.Recovery.SubstrateReport, tc.Equals, report)
}

func (s *recoveryCheckerSuite) TestRecoveryPreparationRefusesUnfencedSource(c *tc.C) {
	env := &preparableRecoveryEnviron{err: errors.New("source controller is not fenced")}
	args := recoveryBootstrapArgs()
	ctx := environscmd.BootstrapContext(c.Context(), cmdtesting.Context(c))
	err := prepareControllerEnvironment(ctx, env, PrepareParams{Recovery: args.Recovery}, true)
	c.Check(err, tc.ErrorMatches, "source controller is not fenced")
	c.Check(env.standardCalled, tc.IsFalse)
	c.Check(args.Recovery.SubstrateReport, tc.IsNil)
}

func (s *recoveryCheckerSuite) TestRecoveryPreparationRequiresProviderCapability(c *tc.C) {
	args := recoveryBootstrapArgs()
	ctx := environscmd.BootstrapContext(c.Context(), cmdtesting.Context(c))
	err := prepareControllerEnvironment(ctx, &recoveryEnviron{}, PrepareParams{Recovery: args.Recovery}, true)
	c.Check(err, tc.ErrorMatches, ".*recovery preparation.*not supported")
}
