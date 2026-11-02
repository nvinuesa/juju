// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package vsphere

import (
	"context"
	"path"

	"github.com/juju/juju/environs"
	"github.com/juju/juju/internal/provider/common"
)

// PrepareForRecovery validates access to the existing source environment.
// It creates no provider resources.
func (env *environ) PrepareForRecovery(ctx environs.BootstrapContext, params environs.RecoverySubstrateParams) (*environs.RecoverySubstrateReport, error) {
	err := func() error {
		return env.withSession(ctx, func(session *sessionEnviron) error {
			_, err := session.client.FindFolder(session.ctx, path.Join(session.getVMFolder(), controllerFolderName(params.ControllerUUID), session.modelFolderName()))
			return session.handleCredentialError(ctx, err)
		})
	}()
	return &environs.RecoverySubstrateReport{}, err
}

// ProvisionRecoveryMachine retains the provider's finalisation lifecycle and
// limits cleanup to the replacement, using a distinct physical resource name.
func (env *environ) ProvisionRecoveryMachine(ctx environs.BootstrapContext, params environs.RecoveryMachineParams) (*environs.BootstrapResult, func(context.Context) error, error) {
	return common.ProvisionRecoveryMachine(ctx, env, params, func(ctx environs.BootstrapContext, args environs.BootstrapParams) (*environs.BootstrapResult, error) {
		return Bootstrap(ctx, env, args)
	})
}

var _ environs.RecoveryMachineProvisioner = (*environ)(nil)
