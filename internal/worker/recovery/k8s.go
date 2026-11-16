// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package recovery

import (
	"context"
	"os"

	coreapplication "github.com/juju/juju/core/application"
	coremodel "github.com/juju/juju/core/model"
	"github.com/juju/juju/core/providertracker"
	"github.com/juju/juju/core/unit"
	"github.com/juju/juju/internal/errors"
	k8sconstants "github.com/juju/juju/internal/provider/kubernetes/constants"
	"github.com/juju/juju/internal/services"
	"github.com/juju/juju/internal/worker/bootstrap"
)

// finaliseK8sController rebinds the replacement controller application and
// unit passwords, persists its introduction nonce, records the replacement
// Service and publishes its API host ports.
func finaliseK8sController(
	ctx context.Context,
	controller services.ControllerDomainServices,
	modelServices services.ModelDomainServices,
	factory providertracker.ProviderFactory,
	controllerModelUUID string,
	cfg ManifoldConfig,
) error {
	appPassword := os.Getenv(k8sconstants.EnvJujuK8sApplicationPassword)
	unitPassword := os.Getenv(k8sconstants.EnvJujuK8sUnitPassword)
	if appPassword == "" || unitPassword == "" {
		return errors.New("missing replacement controller application or unit password")
	}
	if err := finaliseK8sPasswords(ctx, modelServices.AgentPassword(), modelServices.Application(),
		k8sPasswordConfig{
			ControllerID:        cfg.ControllerID,
			AgentPassword:       cfg.AgentPassword,
			ApplicationPassword: appPassword,
			UnitPassword:        unitPassword,
			NoncePath:           k8sconstants.ControllerNonceFilePath,
		}); err != nil {
		return errors.Capture(err)
	}

	manager, err := providertracker.ProviderRunner[bootstrap.ServiceManager](
		factory, controllerModelUUID)(ctx)
	if err != nil {
		return errors.Capture(err)
	}
	service, err := manager.GetService(ctx, k8sconstants.JujuControllerStackName, true)
	if err != nil {
		return errors.Capture(err)
	}
	if service == nil || service.Id == "" || len(service.Addresses) == 0 {
		return errors.New("replacement controller service has no API addresses")
	}
	if err := modelServices.Application().UpdateK8sService(ctx,
		k8sconstants.JujuControllerStackName, service.Id, service.Addresses); err != nil {
		return errors.Capture(err)
	}
	config, err := controller.ControllerConfig().ControllerConfig(ctx)
	if err != nil {
		return errors.Capture(err)
	}
	return bootstrap.InitialiseAPIHostPorts(ctx, controller.ControllerNode(),
		modelServices.Network(),
		func(context.Context) (bootstrap.ServiceManager, error) { return manager, nil },
		cfg.ControllerID, coremodel.CAAS, config, service.Addresses, cfg.APIPort)
}

type k8sPasswordConfig struct {
	ControllerID, AgentPassword, ApplicationPassword, UnitPassword, NoncePath string
}

type k8sControllerApplication interface {
	GetApplicationUUIDByName(context.Context, string) (coreapplication.UUID, error)
}

func finaliseK8sPasswords(
	ctx context.Context,
	passwords bootstrap.AgentPasswordService,
	applications k8sControllerApplication,
	cfg k8sPasswordConfig,
) error {
	nonce, err := os.ReadFile(cfg.NoncePath)
	if err != nil {
		return errors.Errorf("reading replacement introduction nonce: %w", err)
	}
	if len(nonce) == 0 {
		return errors.New("empty replacement introduction nonce")
	}
	applicationUUID, err := applications.GetApplicationUUIDByName(ctx, k8sconstants.JujuControllerStackName)
	if err != nil {
		return errors.Capture(err)
	}
	if err := passwords.SetApplicationPassword(ctx, applicationUUID, cfg.ApplicationPassword); err != nil {
		return errors.Capture(err)
	}
	if err := passwords.SetUnitPassword(
		ctx, unit.Name(k8sconstants.JujuControllerStackName+"/"+cfg.ControllerID), cfg.UnitPassword,
	); err != nil {
		return errors.Capture(err)
	}
	if err := passwords.SetControllerNodePassword(ctx, cfg.ControllerID, cfg.AgentPassword); err != nil {
		return errors.Capture(err)
	}
	persisted, err := passwords.EnsureControllerNodeNonce(ctx, cfg.ControllerID, string(nonce))
	if err != nil {
		return errors.Capture(err)
	}
	if persisted != string(nonce) {
		return errors.New("replacement introduction nonce disagrees with persisted nonce")
	}
	return nil
}
