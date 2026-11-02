// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package ec2

import (
	"context"

	"github.com/juju/juju/environs"
	"github.com/juju/juju/internal/provider/common"
)

// PrepareForRecovery validates access to the existing source environment.
// It creates no provider resources.
func (env *environ) PrepareForRecovery(ctx environs.BootstrapContext, params environs.RecoverySubstrateParams) (*environs.RecoverySubstrateReport, error) {
	err := env.PrepareForBootstrap(ctx, params.ControllerName)
	return &environs.RecoverySubstrateReport{}, err
}

// ProvisionRecoveryMachine retains the provider's finalisation lifecycle and
// limits cleanup to the replacement, using a distinct physical resource name.
func (env *environ) ProvisionRecoveryMachine(ctx environs.BootstrapContext, params environs.RecoveryMachineParams) (*environs.BootstrapResult, func(context.Context) error, error) {
	return common.ProvisionRecoveryMachine(ctx, env, params, env.Bootstrap)
}

var _ environs.RecoveryMachineProvisioner = (*environ)(nil)
