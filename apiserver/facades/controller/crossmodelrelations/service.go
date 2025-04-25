// Copyright 2024 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package crossmodelrelations

import (
	"context"

	"github.com/juju/juju/core/network"
	"github.com/juju/juju/core/secrets"
	"github.com/juju/juju/core/status"
	"github.com/juju/juju/core/unit"
	"github.com/juju/juju/core/watcher"
	"github.com/juju/juju/environs/config"
)

// The following interfaces are used to access secret services.

type SecretService interface {
	GetSecret(ctx context.Context, uri *secrets.URI) (*secrets.SecretMetadata, error)
	WatchRemoteConsumedSecretsChanges(ctx context.Context, appName string) (watcher.StringsWatcher, error)
}

// ModelConfigService is an interface that provides access to the
// model configuration.
type ModelConfigService interface {
	ModelConfig(ctx context.Context) (*config.Config, error)
	Watch() (watcher.StringsWatcher, error)
}

// ApplicationService instances implement an application service.
type ApplicationService interface {
	// GetPublicAddress returns the public address for the specified unit.
	// For k8s provider, it will return the first public address of the cloud
	// service if any, the first public address of the cloud container otherwise.
	// For machines provider, it will return the first public address of the
	// machine.
	//
	// The following errors may be returned:
	// - [uniterrors.UnitNotFound] if the unit does not exist
	GetPublicAddress(ctx context.Context, unitName unit.Name) (network.SpaceAddress, error)
}

type StatusService interface {
	// GetApplicationDisplayStatus returns the display status of the specified application.
	// The display status is equal to the application status if it is set, otherwise it is
	// derived from the unit display statuses.
	// If no application is found, an error satisfying [statuserrors.ApplicationNotFound]
	// is returned.
	GetApplicationDisplayStatus(context.Context, string) (status.StatusInfo, error)
}
