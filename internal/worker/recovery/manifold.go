// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package recovery

import (
	"context"

	"github.com/juju/worker/v5"
	"github.com/juju/worker/v5/dependency"

	"github.com/juju/juju/core/machine"
	"github.com/juju/juju/core/model"
	"github.com/juju/juju/core/providertracker"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/internal/controllerinit"
	"github.com/juju/juju/internal/errors"
	internalrecovery "github.com/juju/juju/internal/recovery"
	"github.com/juju/juju/internal/services"
	"github.com/juju/juju/internal/worker/gate"
)

// ManifoldConfig supplies startup values directly from the provisioned agent.
type ManifoldConfig struct {
	GateName, DomainServicesName, ProviderFactoryName string
	DataDir, AgentPassword, ControllerID              string
	APIPort                                           int
}

func Manifold(cfg ManifoldConfig) dependency.Manifold {
	return dependency.Manifold{
		Inputs: []string{cfg.GateName, cfg.DomainServicesName, cfg.ProviderFactoryName},
		Start: func(ctx context.Context, getter dependency.Getter) (worker.Worker, error) {
			if cfg.GateName == "" || cfg.DomainServicesName == "" || cfg.ProviderFactoryName == "" || cfg.DataDir == "" || cfg.AgentPassword == "" || cfg.ControllerID == "" || cfg.APIPort == 0 {
				return nil, errors.New("incomplete recovery manifold configuration")
			}
			params, err := internalrecovery.ReadParams(cfg.DataDir)
			if err != nil {
				return nil, errors.Capture(err)
			}
			var unlocker gate.Unlocker
			if err := getter.Get(cfg.GateName, &unlocker); err != nil {
				return nil, err
			}
			var controller services.ControllerDomainServices
			if err := getter.Get(cfg.DomainServicesName, &controller); err != nil {
				return nil, err
			}
			var domains services.DomainServicesGetter
			if err := getter.Get(cfg.DomainServicesName, &domains); err != nil {
				return nil, err
			}
			var factory providertracker.ProviderFactory
			if err := getter.Get(cfg.ProviderFactoryName, &factory); err != nil {
				return nil, err
			}
			controllerModel, err := controller.Model().ControllerModel(ctx)
			if err != nil {
				return nil, errors.Capture(err)
			}
			modelServices, err := domains.ServicesForModel(ctx, controllerModel.UUID)
			if err != nil {
				return nil, errors.Capture(err)
			}
			operation := func(ctx context.Context) error {
				if controllerModel.ModelType == model.CAAS {
					return finaliseK8sController(ctx, controller, modelServices, factory, controllerModel.UUID, cfg)
				}
				if controllerModel.ModelType != model.IAAS {
					return errors.New("unsupported recovery model type")
				}
				name := machine.Name(params.MachineName)
				machineID, err := modelServices.Machine().GetMachineUUID(ctx, name)
				if err != nil {
					return errors.Capture(err)
				}
				id, displayName, err := modelServices.Machine().GetInstanceIDAndName(ctx, machineID)
				if err != nil {
					return errors.Capture(err)
				}
				if id != params.Node.BootstrapMachineInstanceId || displayName != params.Node.BootstrapMachineDisplayName {
					return errors.New("recovered machine does not describe the replacement instance")
				}
				if err := modelServices.AgentPassword().SetMachinePassword(ctx, name, cfg.AgentPassword); err != nil {
					return errors.Capture(err)
				}
				if err := modelServices.AgentPassword().SetControllerNodePassword(ctx, cfg.ControllerID, cfg.AgentPassword); err != nil {
					return errors.Capture(err)
				}
				provider, err := providertracker.ProviderRunner[environs.BootstrapEnviron](factory, controllerModel.UUID.String())(ctx)
				if err != nil {
					return errors.Capture(err)
				}
				finder, err := environs.NewBootstrapAddressFinder(provider, params.Node.BootstrapMachineInstanceId)
				if err != nil {
					return errors.Capture(err)
				}
				addresses, err := finder.BootstrapControllerAddresses(ctx)
				if err != nil {
					return errors.Capture(err)
				}
				config, err := controller.ControllerConfig().ControllerConfig(ctx)
				if err != nil {
					return errors.Capture(err)
				}
				if err := controllerinit.InitialiseAPIHostPorts(ctx, controller.ControllerNode(), modelServices.Network(), nil, cfg.ControllerID, controllerModel.ModelType, config, addresses, cfg.APIPort); err != nil {
					return errors.Capture(err)
				}
				return controllerinit.DeleteSSHKeys(params.Node.BootstrapSSHAuthorizedKeys)
			}
			return NewWorker(WorkerConfig{ArchivePath: params.ArchivePath, DataDir: cfg.DataDir, Checksum: params.SHA256, Operation: operation, FlagService: controller.Flag(), Unlocker: unlocker})
		},
	}
}
