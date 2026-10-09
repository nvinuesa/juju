// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package bootstrap

import (
	"context"

	"github.com/juju/juju/controller"
	"github.com/juju/juju/core/machine"
	coremodel "github.com/juju/juju/core/model"
	"github.com/juju/juju/core/network"
	"github.com/juju/juju/internal/cloudconfig/instancecfg"
	"github.com/juju/juju/internal/controllerinit"
	"github.com/juju/juju/internal/errors"
)

// FinaliseIAASAgent records the password and provisioned instance for the named
// machine. The caller supplies the agent nonce for that machine.
func FinaliseIAASAgent(
	ctx context.Context,
	agentPasswordService controllerinit.AgentPasswordService,
	machineService MachineService,
	machineName machine.Name,
	machineNonce string,
	bootstrapParams instancecfg.StateInitializationParams,
	agentPassword string,
) error {
	// Locate the machine whose instance data will be recorded.
	machineUUID, err := machineService.GetMachineUUID(ctx, machineName)
	if err != nil {
		return errors.Capture(err)
	}

	// Set the agent password for the machine.
	if err := agentPasswordService.SetMachinePassword(ctx, machineName, agentPassword); err != nil {
		return errors.Capture(err)
	}

	// If this data exists, we consider the machine as provisioned.
	if err := machineService.SetMachineCloudInstance(
		ctx,
		machineUUID,
		bootstrapParams.BootstrapMachineInstanceId,
		bootstrapParams.BootstrapMachineDisplayName,
		machineNonce,
		bootstrapParams.BootstrapMachineHardwareCharacteristics,
	); err != nil {
		return errors.Capture(err)
	}

	return nil
}

// InitialiseAPIHostPorts publishes the initial API address projection for the
// supplied controller.
func InitialiseAPIHostPorts(ctx context.Context, nodes ControllerNodeService,
	networks NetworkService, manager ServiceManagerGetterFunc, id string,
	modelType coremodel.ModelType, cfg controller.Config,
	addresses network.ProviderAddresses, port int) error {
	return controllerinit.InitialiseAPIHostPorts(ctx, nodes, networks,
		func(ctx context.Context) (controllerinit.ServiceManager, error) { return manager(ctx) },
		id, modelType, cfg, addresses, port)
}

func orderBootstrapAddresses(addresses network.SpaceAddresses, scope network.Scope) network.SpaceAddresses {
	return controllerinit.OrderAddresses(addresses, scope)
}
