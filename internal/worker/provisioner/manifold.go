// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package provisioner

import (
	"context"

	"github.com/juju/errors"
	"github.com/juju/worker/v4"
	"github.com/juju/worker/v4/dependency"

	"github.com/juju/juju/agent"
	apiprovisioner "github.com/juju/juju/api/agent/provisioner"
	"github.com/juju/juju/api/base"
	coredependency "github.com/juju/juju/core/dependency"
	"github.com/juju/juju/core/instance"
	"github.com/juju/juju/core/logger"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/internal/servicefactory"
	"github.com/juju/juju/internal/worker/common"
)

// MachineService defines the methods that the worker assumes from the Machine
// service.
type MachineService interface {
	// SetMachineCloudInstance sets an entry in the machine cloud instance table
	// along with the instance tags and the link to a lxd profile if any.
	SetMachineCloudInstance(ctx context.Context, machineUUID string, instanceID instance.Id, hardwareCharacteristics *instance.HardwareCharacteristics) error
}

// GetMachineFunc is a helper function that gets a service from the manifold.
type GetMachineServiceFunc func(getter dependency.Getter, name string) (MachineService, error)

// GetMachineService is a helper function that gets a service from the
// manifold.
func GetMachineService(getter dependency.Getter, name string) (MachineService, error) {
	return coredependency.GetDependencyByName(getter, name, func(factory servicefactory.ModelServiceFactory) MachineService {
		return factory.Machine()
	})
}

// ManifoldConfig defines an environment provisioner's dependencies. It's not
// currently clear whether it'll be easier to extend this type to include all
// provisioners, or to create separate (Environ|Container)Manifold[Config]s;
// for now we dodge the question because we don't need container provisioners
// in dependency engines. Yet.
type ManifoldConfig struct {
	AgentName          string
	APICallerName      string
	EnvironName        string
	ServiceFactoryName string
	GetMachineService  GetMachineServiceFunc
	Logger             logger.Logger

	NewProvisionerFunc           func(ControllerAPI, MachineService, MachinesAPI, ToolsFinder, DistributionGroupFinder, agent.Config, logger.Logger, Environ, common.CredentialAPI) (Provisioner, error)
	NewCredentialValidatorFacade func(base.APICaller) (common.CredentialAPI, error)
}

// Validate is called by start to check for bad configuration.
func (config ManifoldConfig) Validate() error {
	if config.AgentName == "" {
		return errors.NotValidf("empty AgentName")
	}
	if config.APICallerName == "" {
		return errors.NotValidf("empty APICallerName")
	}
	if config.EnvironName == "" {
		return errors.NotValidf("empty EnvironName")
	}
	if config.ServiceFactoryName == "" {
		return errors.NotValidf("empty ServiceFactoryName")
	}
	if config.GetMachineService == nil {
		return errors.NotValidf("nil GetMachineService")
	}
	if config.Logger == nil {
		return errors.NotValidf("nil Logger")
	}
	if config.NewProvisionerFunc == nil {
		return errors.NotValidf("nil NewProvisionerFunc")
	}
	if config.NewCredentialValidatorFacade == nil {
		return errors.NotValidf("nil NewCredentialValidatorFacade")
	}
	return nil
}

// Manifold creates a manifold that runs an environment provisioner. See the
// ManifoldConfig type for discussion about how this can/should evolve.
func Manifold(config ManifoldConfig) dependency.Manifold {
	return dependency.Manifold{
		Inputs: []string{
			config.AgentName,
			config.APICallerName,
			config.EnvironName,
			config.ServiceFactoryName,
		},
		Start: func(ctx context.Context, getter dependency.Getter) (worker.Worker, error) {
			if err := config.Validate(); err != nil {
				return nil, errors.Trace(err)
			}
			var agent agent.Agent
			if err := getter.Get(config.AgentName, &agent); err != nil {
				return nil, errors.Trace(err)
			}

			var apiCaller base.APICaller
			if err := getter.Get(config.APICallerName, &apiCaller); err != nil {
				return nil, errors.Trace(err)
			}

			var environ environs.Environ
			if err := getter.Get(config.EnvironName, &environ); err != nil {
				return nil, errors.Trace(err)
			}

			api := apiprovisioner.NewClient(apiCaller)
			agentConfig := agent.CurrentConfig()

			credentialAPI, err := config.NewCredentialValidatorFacade(apiCaller)
			if err != nil {
				return nil, errors.Trace(err)
			}

			machineService, err := config.GetMachineService(getter, config.ServiceFactoryName)
			if err != nil {
				return nil, errors.Trace(err)
			}

			w, err := config.NewProvisionerFunc(api, machineService, api, api, api, agentConfig, config.Logger, environ, credentialAPI)
			if err != nil {
				return nil, errors.Trace(err)
			}
			return w, nil
		},
	}
}
