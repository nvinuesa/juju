// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package remoterelations

import (
	"context"

	"github.com/juju/juju/core/watcher"
	"github.com/juju/juju/rpc/params"
	coreapplication "github.com/juju/juju/state/application"
)

// ControllerConfigAPI provides the subset of common.ControllerConfigAPI
// required by the remote firewaller facade
type ControllerConfigAPI interface {
	// ControllerConfig returns the controller's configuration.
	ControllerConfig(context.Context) (params.ControllerConfigResult, error)

	// ControllerAPIInfoForModels returns the controller api connection details for the specified models.
	ControllerAPIInfoForModels(ctx context.Context, args params.Entities) (params.ControllerAPIInfoResults, error)
}

// CrossModelRelationService provides access to cross model relation domain methods.
type CrossModelRelationService interface {
	// GetRemoteApplicationUUIDByName returns the application UUID for a remote
	// application by name.
	GetRemoteApplicationUUIDByName(ctx context.Context, name string) (coreapplication.UUID, error)
}

// RelationService defines the methods that the facade assumes from the
// Relation service.
type RelationService interface {
	// WatchApplicationRelationKeysSuspended returns a watcher that notifies of
	// changes to the life or suspended status for any relation the application
	// is part of. The watcher notifies with the relation keys.
	WatchApplicationRelationKeysSuspended(ctx context.Context, applicationUUID coreapplication.UUID) (watcher.StringsWatcher, error)
}
