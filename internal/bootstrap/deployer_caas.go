// Copyright 2023 Canonical Ltd.
// Licensed under the LGPLv3, see LICENCE file for details.

package bootstrap

import (
	"context"
	"fmt"

	"github.com/juju/errors"

	"github.com/juju/juju/caas"
	k8sconstants "github.com/juju/juju/caas/kubernetes/provider/constants"
	corebase "github.com/juju/juju/core/base"
	"github.com/juju/juju/core/network"
	coreunit "github.com/juju/juju/core/unit"
	"github.com/juju/juju/core/version"
	applicationservice "github.com/juju/juju/domain/application/service"
	"github.com/juju/juju/environs/bootstrap"
)

// ServiceManager provides the API to manipulate services.
type ServiceManager interface {
	// GetService returns the service for the specified application.
	GetService(ctx context.Context, appName string, includeClusterIP bool) (*caas.Service, error)
}

// CAASDeployerConfig holds the configuration for a CAASDeployer.
type CAASDeployerConfig struct {
	BaseDeployerConfig
	UnitPassword   string
	ServiceManager ServiceManager
}

// Validate validates the configuration.
func (c CAASDeployerConfig) Validate() error {
	if err := c.BaseDeployerConfig.Validate(); err != nil {
		return errors.Trace(err)
	}
	if c.ServiceManager == nil {
		return errors.NotValidf("ServiceManager")
	}
	return nil
}

// CAASDeployer is the interface that is used to deploy the controller charm
// for CAAS workloads.
type CAASDeployer struct {
	baseDeployer
	unitPassword   string
	serviceManager ServiceManager
}

// NewCAASDeployer returns a new ControllerCharmDeployer for CAAS workloads.
func NewCAASDeployer(config CAASDeployerConfig) (*CAASDeployer, error) {
	if err := config.Validate(); err != nil {
		return nil, errors.Trace(err)
	}
	return &CAASDeployer{
		baseDeployer:   makeBaseDeployer(config.BaseDeployerConfig),
		unitPassword:   config.UnitPassword,
		serviceManager: config.ServiceManager,
	}, nil
}

// ControllerAddress returns the address of the controller that should be
// used.
func (d *CAASDeployer) ControllerAddress(ctx context.Context) (string, error) {
	// Retrieve the k8s service from the k8s broker.
	svc, err := d.serviceManager.GetService(ctx, k8sconstants.JujuControllerStackName, true)
	if err != nil {
		return "", errors.Trace(err)
	}
	// Insert the k8s service..
	if len(svc.Addresses) == 0 {
		// this should never happen because we have already checked in k8s controller bootstrap stacker.
		return "", errors.NotProvisionedf("k8s controller service %q address", svc.Id)
	}

	addrs := d.getAlphaSpaceAddresses(svc.Addresses)
	hp := network.SpaceAddressesWithPort(addrs, 0)
	addr := hp.AllMatchingScope(network.ScopeMatchCloudLocal)

	var controllerAddress string
	if len(addr) > 0 {
		controllerAddress = addr[0]
	}
	d.logger.Debugf(ctx, "CAAS controller address %v", controllerAddress)
	return controllerAddress, nil
}

// getAlphaSpaceAddresses returns a SpaceAddresses created from the input
// providerAddresses and using the alpha space ID as their SpaceID.
// We set all the spaces of the output SpaceAddresses to be the alpha space ID.
func (d *CAASDeployer) getAlphaSpaceAddresses(providerAddresses network.ProviderAddresses) network.SpaceAddresses {
	sas := make(network.SpaceAddresses, len(providerAddresses))
	for i, pa := range providerAddresses {
		sas[i] = network.SpaceAddress{MachineAddress: pa.MachineAddress}
		if pa.SpaceName != "" {
			sas[i].SpaceID = network.AlphaSpaceId
		}
	}
	return sas
}

// ControllerCharmBase returns the base used for deploying the controller
// charm.
func (d *CAASDeployer) ControllerCharmBase() (corebase.Base, error) {
	return version.DefaultSupportedLTSBase(), nil
}

// CompleteProcess is called when the bootstrap process is complete.
func (d *CAASDeployer) CompleteProcess(ctx context.Context, controllerUnit coreunit.Name) error {
	providerID := controllerProviderID(controllerUnit)
	if err := d.applicationService.UpdateCAASUnit(ctx, controllerUnit, applicationservice.UpdateCAASUnitParams{
		ProviderID: &providerID,
	}); err != nil {
		return errors.Annotatef(err, "updating controller unit")
	}
	if err := d.passwordService.SetUnitPassword(ctx, controllerUnit, d.unitPassword); err != nil {
		return errors.Annotate(err, "setting controller unit password")
	}

	// Retrieve the k8s service from the k8s broker.
	svc, err := d.serviceManager.GetService(ctx, k8sconstants.JujuControllerStackName, true)
	if err != nil {
		return errors.Trace(err)
	}
	// Insert the k8s service..
	if len(svc.Addresses) == 0 {
		// this should never happen because we have already checked in k8s controller bootstrap stacker.
		return errors.NotProvisionedf("k8s controller service %q address", svc.Id)
	}

	addrs := d.getAlphaSpaceAddresses(svc.Addresses)
	d.logger.Infof(ctx, "creating cloud service for k8s controller %q", controllerProviderID(controllerUnit))
	err = d.applicationService.UpdateCloudService(ctx, bootstrap.ControllerApplicationName, controllerProviderID(controllerUnit), addrs)
	if err != nil {
		return errors.Trace(err)
	}
	d.logger.Criticalf(ctx, "created cloud service %v for controller", svc)

	return nil
}

func controllerProviderID(name coreunit.Name) string {
	return fmt.Sprintf("controller-%d", name.Number())
}
