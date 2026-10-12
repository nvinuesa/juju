// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package azure

import (
	"context"

	"github.com/juju/juju/core/instance"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/internal/errors"
	"github.com/juju/juju/internal/provider/azure/internal/errorutils"
	"github.com/juju/juju/internal/provider/common"
)

// PrepareForRecovery validates source resources without creating or changing
// the resource group, network or availability set.
func (env *azureEnviron) PrepareForRecovery(ctx environs.BootstrapContext, params environs.RecoverySubstrateParams) (*environs.RecoverySubstrateReport, error) {
	if err := env.PrepareForBootstrap(ctx, params.ControllerName); err != nil {
		return nil, err
	}
	if err := env.validateResourceGroup(ctx, env.resourceGroup); err != nil {
		return nil, err
	}
	env.mu.Lock()
	env.commonResourcesCreated = true
	env.mu.Unlock()
	return &environs.RecoverySubstrateReport{}, nil
}

// ProvisionRecoveryMachine uses instance provisioning without Bootstrap's
// model-destruction rollback. Azure ARM deployments otherwise update resources
// by name, so reject an existing replacement name before any write.
func (env *azureEnviron) ProvisionRecoveryMachine(ctx environs.BootstrapContext, params environs.RecoveryMachineParams) (*environs.BootstrapResult, func(context.Context) error, error) {
	name, err := env.namespace.Hostname(params.MachineID)
	if err != nil {
		return nil, nil, err
	}
	client, err := env.computeClient()
	if err != nil {
		return nil, nil, err
	}
	_, err = client.Get(ctx, env.resourceGroup, name, nil)
	if err == nil {
		return nil, nil, errors.New("replacement instance already exists")
	}
	if !errorutils.IsNotFoundError(err) {
		return nil, nil, errors.Capture(err)
	}
	result, cleanup, err := common.ProvisionRecoveryMachine(ctx, env, params,
		func(ctx environs.BootstrapContext, args environs.BootstrapParams) (*environs.BootstrapResult, error) {
			return common.Bootstrap(ctx, env, args)
		})
	if err != nil {
		// A deployment may exist before StartInstance reports an instance ID.
		cleanup = func(ctx context.Context) error { return env.StopInstances(ctx, instance.Id(name)) }
	}
	return result, cleanup, err
}

var _ environs.RecoveryMachineProvisioner = (*azureEnviron)(nil)
