// Copyright 2017 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package crossmodelrelations

import (
	"context"

	"github.com/juju/errors"

	apiservererrors "github.com/juju/juju/apiserver/errors"
	"github.com/juju/juju/apiserver/internal"
	"github.com/juju/juju/core/watcher"
	"github.com/juju/juju/internal/worker/watcherregistry"
	"github.com/juju/juju/rpc/params"
)

// CrossModelRelationsService provides access to the crossmodelrelation domain service.
type CrossModelRelationsService interface {
	// WatchConsumedSecretsChanges watches secrets consumed by the specified remote
	// application and returns a watcher which notifies of secret URIs that have had
	// a new revision added.
	WatchConsumedSecretsChanges(ctx context.Context, appName string) (watcher.SecretTriggerWatcher, error)
}

// CrossModelRelationsAPIv3 provides access to the CrossModelRelations API facade.
type CrossModelRelationsAPIv3 struct {
	watcherRegistry watcherregistry.WatcherRegistry
	service         CrossModelRelationsService
}

// NewCrossModelRelationsAPI returns a new server-side CrossModelRelationsAPI facade.
func NewCrossModelRelationsAPI(
	watcherRegistry watcherregistry.WatcherRegistry,
	service         CrossModelRelationsService,
) (*CrossModelRelationsAPIv3, error) {
	return &CrossModelRelationsAPIv3{
		watcherRegistry: watcherRegistry,
		service:         service,
	}, nil
}

// PublishRelationChanges publishes relation changes to the
// model hosting the remote application involved in the relation.
func (api *CrossModelRelationsAPIv3) PublishRelationChanges(
	ctx context.Context,
	changes params.RemoteRelationsChanges,
) (params.ErrorResults, error) {
	return params.ErrorResults{}, nil
}

// RegisterRemoteRelations sets up the model to participate
// in the specified relations. This operation is idempotent.
func (api *CrossModelRelationsAPIv3) RegisterRemoteRelations(
	ctx context.Context,
	relations params.RegisterRemoteRelationArgs,
) (params.RegisterRemoteRelationResults, error) {
	return params.RegisterRemoteRelationResults{}, nil
}

// WatchRelationChanges starts a RemoteRelationChangesWatcher for each
// specified relation, returning the watcher IDs and initial values,
// or an error if the remote relations couldn't be watched.
func (api *CrossModelRelationsAPIv3) WatchRelationChanges(ctx context.Context, remoteRelationArgs params.RemoteEntityArgs) (
	params.RemoteRelationWatchResults, error,
) {
	return params.RemoteRelationWatchResults{}, nil
}

// WatchRelationsSuspendedStatus starts a RelationStatusWatcher for
// watching the life and suspended status of a relation.
func (api *CrossModelRelationsAPIv3) WatchRelationsSuspendedStatus(
	ctx context.Context,
	remoteRelationArgs params.RemoteEntityArgs,
) (params.RelationStatusWatchResults, error) {
	return params.RelationStatusWatchResults{}, nil
}

// WatchOfferStatus starts an OfferStatusWatcher for
// watching the status of an offer.
func (api *CrossModelRelationsAPIv3) WatchOfferStatus(
	ctx context.Context,
	offerArgs params.OfferArgs,
) (params.OfferStatusWatchResults, error) {
	return params.OfferStatusWatchResults{}, nil
}

// WatchConsumedSecretsChanges returns a watcher which notifies of changes to any secrets
// for the specified remote consumers.
func (api *CrossModelRelationsAPIv3) WatchConsumedSecretsChanges(ctx context.Context, args params.WatchRemoteSecretChangesArgs) (params.SecretRevisionWatchResults, error) {
	results := params.SecretRevisionWatchResults{
		Results: make([]params.SecretRevisionWatchResult, len(args.Args)),
	}

	// TODO: Add proper authentication/authorization using macaroons
	// For now, this is a basic implementation without auth

	for i, arg := range args.Args {
		// For now, we use the application token as the app name
		// TODO: Properly extract the app name from token after implementing
		// GetSecretConsumerInfo in the state layer
		appName := arg.ApplicationToken

		w, err := api.service.WatchConsumedSecretsChanges(ctx, appName)
		if err != nil {
			results.Results[i].Error = apiservererrors.ServerError(err)
			continue
		}

		// Get initial changes
		select {
		case changes, ok := <-w.Changes():
			if !ok {
				results.Results[i].Error = apiservererrors.ServerError(errors.New("watcher closed unexpectedly"))
				w.Kill()
				continue
			}

			// Convert to params
			paramChanges := make([]params.SecretRevisionChange, len(changes))
			for j, c := range changes {
				paramChanges[j] = params.SecretRevisionChange{
					URI:            c.URI.String(),
					LatestRevision: c.Revision,
				}
			}

			watcherId, _, err := internal.EnsureRegisterWatcher(ctx, api.watcherRegistry, w)
			if err != nil {
				results.Results[i].Error = apiservererrors.ServerError(err)
				w.Kill()
				continue
			}

			results.Results[i] = params.SecretRevisionWatchResult{
				WatcherId: watcherId,
				Changes:   paramChanges,
			}
		case <-ctx.Done():
			results.Results[i].Error = apiservererrors.ServerError(ctx.Err())
			w.Kill()
			continue
		}
	}

	return results, nil
}

// PublishIngressNetworkChanges publishes changes to the required
// ingress addresses to the model hosting the offer in the relation.
func (api *CrossModelRelationsAPIv3) PublishIngressNetworkChanges(
	ctx context.Context,
	changes params.IngressNetworksChanges,
) (params.ErrorResults, error) {
	return params.ErrorResults{}, nil
}

// WatchEgressAddressesForRelations creates a watcher that notifies when addresses, from which
// connections will originate for the relation, change.
// Each event contains the entire set of addresses which are required for ingress for the relation.
func (api *CrossModelRelationsAPIv3) WatchEgressAddressesForRelations(ctx context.Context, remoteRelationArgs params.RemoteEntityArgs) (params.StringsWatchResults, error) {
	return params.StringsWatchResults{}, nil
}
