// Copyright 2024 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package caasapplicationprovisioner

import (
	"context"

	"github.com/juju/juju/controller"
	"github.com/juju/juju/core/application"
	coreconfig "github.com/juju/juju/core/config"
	"github.com/juju/juju/core/leadership"
	"github.com/juju/juju/core/life"
	"github.com/juju/juju/core/model"
	"github.com/juju/juju/core/unit"
	"github.com/juju/juju/core/watcher"
	applicationcharm "github.com/juju/juju/domain/application/charm"
	"github.com/juju/juju/domain/application/service"
	"github.com/juju/juju/environs/config"
	internalcharm "github.com/juju/juju/internal/charm"
	"github.com/juju/juju/internal/charm/resource"
)

// ControllerConfigService provides the controller configuration.
type ControllerConfigService interface {
	// ControllerConfig returns the config values for the controller.
	ControllerConfig(ctx context.Context) (controller.Config, error)
	// WatchControllerConfig returns a watcher that returns keys for any
	// changes to controller config.
	WatchControllerConfig() (watcher.StringsWatcher, error)
}

// ModelConfigService provides access to the model configuration.
type ModelConfigService interface {
	// ModelConfig returns the current config for the model.
	ModelConfig(context.Context) (*config.Config, error)
	// Watch returns a watcher that returns keys for any changes to model
	// config.
	Watch() (watcher.StringsWatcher, error)
}

// ModelInfoService describe the service for interacting and reading the underlying
// model information.
type ModelInfoService interface {
	// GetModelInfo returns the readonly model information for the model in
	// question.
	GetModelInfo(context.Context) (model.ModelInfo, error)
}

// ApplicationService describes the service for accessing application scaling info.
type ApplicationService interface {
	// GetApplicationScale returns the desired scale of an application, returning an error
	// satisfying [applicationerrors.ApplicationNotFoundError] if the application doesn't exist.
	// This is used on CAAS models.
	GetApplicationScale(ctx context.Context, appName string) (int, error)
	// GetApplicationConfig returns the application config attributes for the
	// configuration.
	// If no application is found, an error satisfying
	// [applicationerrors.ApplicationNotFound] is returned.
	GetApplicationConfig(ctx context.Context, appID application.ID) (coreconfig.ConfigAttributes, error)
	// GetApplicationIDByName returns a application ID by application name. It
	// returns an error if the application can not be found by the name.
	GetApplicationIDByName(ctx context.Context, name string) (application.ID, error)

	SetApplicationScalingState(ctx context.Context, name string, scaleTarget int, scaling bool) error
	GetApplicationScalingState(ctx context.Context, name string) (service.ScalingState, error)
	GetApplicationLife(ctx context.Context, name string) (life.Value, error)
	GetUnitLife(context.Context, unit.Name) (life.Value, error)
	// GetCharmLocatorByApplicationName returns a CharmLocator by application name.
	// It returns an error if the charm can not be found by the name. This can also
	// be used as a cheap way to see if a charm exists without needing to load the
	// charm metadata.
	GetCharmLocatorByApplicationName(ctx context.Context, name string) (applicationcharm.CharmLocator, error)
	// GetCharmMetadataStorage returns the storage specification for the charm using
	// the charm name, source and revision.
	GetCharmMetadataStorage(ctx context.Context, locator applicationcharm.CharmLocator) (map[string]internalcharm.Storage, error)
	// GetCharmMetadataResources returns the specifications for the resources for the
	// charm using the charm name, source and revision.
	GetCharmMetadataResources(ctx context.Context, locator applicationcharm.CharmLocator) (map[string]resource.Meta, error)
	// IsCharmAvailable returns whether the charm is available for use. This
	// indicates if the charm has been uploaded to the controller.
	// This will return true if the charm is available, and false otherwise.
	IsCharmAvailable(ctx context.Context, locator applicationcharm.CharmLocator) (bool, error)
	DestroyUnit(context.Context, unit.Name) error
	RemoveUnit(context.Context, unit.Name, leadership.Revoker) error
	UpdateCAASUnit(context.Context, unit.Name, service.UpdateCAASUnitParams) error
}
