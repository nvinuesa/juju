// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package recovery

import (
	"context"
	"os"

	"github.com/juju/juju/caas"
	"github.com/juju/juju/core/application"
	"github.com/juju/juju/core/model"
	"github.com/juju/juju/core/providertracker"
	"github.com/juju/juju/core/unit"
	"github.com/juju/juju/internal/controllerinit"
	"github.com/juju/juju/internal/errors"
	k8sconstants "github.com/juju/juju/internal/provider/kubernetes/constants"
	"github.com/juju/juju/internal/services"
)

type k8sServiceManager interface {
	GetService(context.Context, string, bool) (*caas.Service, error)
	ControllerUnitFQDN(int) string
}

func finaliseK8sController(ctx context.Context, controller services.ControllerDomainServices,
	modelServices services.ModelDomainServices, factory providertracker.ProviderFactory,
	modelUUID model.UUID, cfg ManifoldConfig) error {
	if err := finaliseK8sPasswords(ctx, modelServices.AgentPassword(), modelServices.Application(),
		k8sPasswordConfig{ControllerID: cfg.ControllerID, AgentPassword: cfg.AgentPassword,
			ApplicationPassword: os.Getenv(k8sconstants.EnvJujuK8sApplicationPassword),
			UnitPassword:        os.Getenv(k8sconstants.EnvJujuK8sUnitPassword), NoncePath: k8sconstants.ControllerNonceFilePath}); err != nil {
		return errors.Capture(err)
	}
	manager, err := providertracker.ProviderRunner[k8sServiceManager](factory, modelUUID.String())(ctx)
	if err != nil {
		return errors.Capture(err)
	}
	service, err := manager.GetService(ctx, "controller", true)
	if err != nil {
		return errors.Capture(err)
	}
	if service == nil || len(service.Addresses) == 0 {
		return errors.New("replacement controller service has no API addresses")
	}
	config, err := controller.ControllerConfig().ControllerConfig(ctx)
	if err != nil {
		return errors.Capture(err)
	}
	return controllerinit.InitialiseAPIHostPorts(ctx, controller.ControllerNode(), modelServices.Network(),
		func(context.Context) (controllerinit.ServiceManager, error) { return manager, nil },
		cfg.ControllerID, model.CAAS, config, service.Addresses, cfg.APIPort)
}

type controllerApplication interface {
	GetApplicationUUIDByName(context.Context, string) (application.UUID, error)
}

type k8sPasswordConfig struct {
	ControllerID, AgentPassword, ApplicationPassword, UnitPassword, NoncePath string
}

func finaliseK8sPasswords(ctx context.Context, passwords controllerinit.AgentPasswordService, applications controllerApplication, cfg k8sPasswordConfig) error {
	if cfg.ApplicationPassword == "" || cfg.UnitPassword == "" {
		return errors.New("missing replacement controller application or unit password")
	}
	nonce, err := os.ReadFile(cfg.NoncePath)
	if err != nil {
		return errors.Errorf("reading replacement introduction nonce: %w", err)
	}
	if len(nonce) == 0 {
		return errors.New("empty replacement introduction nonce")
	}
	applicationUUID, err := applications.GetApplicationUUIDByName(ctx, "controller")
	if err != nil {
		return errors.Capture(err)
	}
	if err := passwords.SetApplicationPassword(ctx, applicationUUID, cfg.ApplicationPassword); err != nil {
		return errors.Capture(err)
	}
	if err := passwords.SetUnitPassword(ctx, unit.Name("controller/"+cfg.ControllerID), cfg.UnitPassword); err != nil {
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
