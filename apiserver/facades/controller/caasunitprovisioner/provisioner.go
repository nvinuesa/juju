// Copyright 2017 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package caasunitprovisioner

import (
	"context"

	"github.com/juju/clock"
	"github.com/juju/errors"
	"github.com/juju/names/v6"

	apiservererrors "github.com/juju/juju/apiserver/errors"
	"github.com/juju/juju/apiserver/facade"
	"github.com/juju/juju/apiserver/internal"
	"github.com/juju/juju/core/application"
	coreapplication "github.com/juju/juju/core/application"
	"github.com/juju/juju/core/config"
	corelogger "github.com/juju/juju/core/logger"
	"github.com/juju/juju/core/network"
	"github.com/juju/juju/core/watcher"
	applicationerrors "github.com/juju/juju/domain/application/errors"
	"github.com/juju/juju/rpc/params"
	statewatcher "github.com/juju/juju/state/watcher"
)

// NetworkService is the interface that is used to interact with the
// network spaces/subnets.
type NetworkService interface {
	// GetAllSpaces returns all spaces for the model.
	GetAllSpaces(ctx context.Context) (network.SpaceInfos, error)
}

// ApplicationService is used to interact with the application service.
type ApplicationService interface {
	// GetApplicationScale returns the desired scale of an application, returning an error
	// satisfying [applicationerrors.ApplicationNotFoundError] if the application doesn't exist.
	// This is used on CAAS models.
	GetApplicationScale(ctx context.Context, appName string) (int, error)
	// SetApplicationScale sets the application's desired scale value, returning an error
	// satisfying [applicationerrors.ApplicationNotFound] if the application is not found.
	// This is used on CAAS models.
	SetApplicationScale(ctx context.Context, appName string, scale int) error
	// UpdateCloudService updates the cloud service for the specified application, returning an error
	// satisfying [applicationerrors.ApplicationNotFoundError] if the application doesn't exist.
	UpdateCloudService(ctx context.Context, appName, providerID string, sAddrs network.SpaceAddresses) error
	// WatchApplicationScale returns a watcher that observes changes to an application's scale.
	WatchApplicationScale(ctx context.Context, appName string) (watcher.NotifyWatcher, error)
	// GetApplicationConfig returns the application config attributes for the
	// configuration.
	// If no application is found, an error satisfying
	// [applicationerrors.ApplicationNotFound] is returned.
	GetApplicationConfig(ctx context.Context, appID application.ID) (config.ConfigAttributes, error)
	// GetApplicationIDByName returns a application ID by application name. It
	// returns an error if the application can not be found by the name.
	GetApplicationIDByName(ctx context.Context, name string) (application.ID, error)
}

type Facade struct {
	watcherRegistry facade.WatcherRegistry

	networkService     NetworkService
	applicationService ApplicationService
	resources          facade.Resources
	state              CAASUnitProvisionerState
	clock              clock.Clock
	logger             corelogger.Logger
}

// NewFacade returns a new CAAS unit provisioner Facade facade.
func NewFacade(
	watcherRegistry facade.WatcherRegistry,
	resources facade.Resources,
	authorizer facade.Authorizer,
	networkService NetworkService,
	applicationService ApplicationService,
	st CAASUnitProvisionerState,
	clock clock.Clock,
	logger corelogger.Logger,
) (*Facade, error) {
	if !authorizer.AuthController() {
		return nil, apiservererrors.ErrPerm
	}
	return &Facade{
		watcherRegistry:    watcherRegistry,
		networkService:     networkService,
		applicationService: applicationService,
		resources:          resources,
		state:              st,
		clock:              clock,
		logger:             logger,
	}, nil
}

// WatchApplicationsScale starts a NotifyWatcher to watch changes
// to the applications' scale.
func (f *Facade) WatchApplicationsScale(ctx context.Context, args params.Entities) (params.NotifyWatchResults, error) {
	results := params.NotifyWatchResults{
		Results: make([]params.NotifyWatchResult, len(args.Entities)),
	}
	for i, arg := range args.Entities {
		id, err := f.watchApplicationScale(ctx, arg.Tag)
		if err != nil {
			results.Results[i].Error = apiservererrors.ServerError(err)
			continue
		}
		results.Results[i].NotifyWatcherId = id
	}
	return results, nil
}

func (f *Facade) watchApplicationScale(ctx context.Context, tagString string) (string, error) {
	tag, err := names.ParseApplicationTag(tagString)
	if err != nil {
		return "", errors.Trace(err)
	}
	w, err := f.applicationService.WatchApplicationScale(ctx, tag.Id())
	if err != nil {
		return "", errors.Trace(err)
	}
	notifyWatcherId, _, err := internal.EnsureRegisterWatcher(ctx, f.watcherRegistry, w)
	return notifyWatcherId, errors.Trace(err)
}

func (f *Facade) ApplicationsScale(ctx context.Context, args params.Entities) (params.IntResults, error) {
	results := params.IntResults{
		Results: make([]params.IntResult, len(args.Entities)),
	}
	for i, arg := range args.Entities {
		scale, err := f.applicationScale(ctx, arg.Tag)
		if err != nil {
			results.Results[i].Error = apiservererrors.ServerError(err)
			continue
		}
		results.Results[i].Result = scale
	}
	f.logger.Debugf("application scale result: %#v", results)
	return results, nil
}

func (f *Facade) applicationScale(ctx context.Context, tagString string) (int, error) {
	appTag, err := names.ParseApplicationTag(tagString)
	if err != nil {
		return 0, errors.Trace(err)
	}
	scale, err := f.applicationService.GetApplicationScale(ctx, appTag.Id())
	if err != nil {
		return 0, errors.Trace(err)
	}
	return scale, nil
}

// ApplicationsTrust returns the trust status for specified applications in this model.
func (f *Facade) ApplicationsTrust(ctx context.Context, args params.Entities) (params.BoolResults, error) {
	results := params.BoolResults{
		Results: make([]params.BoolResult, len(args.Entities)),
	}
	for i, arg := range args.Entities {
		trust, err := f.applicationTrust(ctx, arg.Tag)
		if err != nil {
			results.Results[i].Error = apiservererrors.ServerError(err)
			continue
		}
		results.Results[i].Result = trust
	}
	f.logger.Debugf("application trust result: %#v", results)
	return results, nil
}

func (f *Facade) applicationTrust(ctx context.Context, tagString string) (bool, error) {
	appTag, err := names.ParseApplicationTag(tagString)
	if err != nil {
		return false, errors.Trace(err)
	}
	appID, err := f.applicationService.GetApplicationIDByName(ctx, appTag.Id())
	if errors.Is(err, applicationerrors.ApplicationNotFound) {
		return false, errors.NotFoundf("application %s", appTag.Id())
	}
	if err != nil {
		return false, errors.Trace(err)
	}
	cfg, err := f.applicationService.GetApplicationConfig(ctx, appID)
	if err != nil {
		return false, errors.Trace(err)
	}
	return cfg.GetBool(coreapplication.TrustConfigOptionName, false), nil
}

// WatchApplicationsTrustHash starts a StringsWatcher to watch changes
// to the applications' trust status.
func (f *Facade) WatchApplicationsTrustHash(ctx context.Context, args params.Entities) (params.StringsWatchResults, error) {
	results := params.StringsWatchResults{
		Results: make([]params.StringsWatchResult, len(args.Entities)),
	}
	for i, arg := range args.Entities {
		id, err := f.watchApplicationTrustHash(arg.Tag)
		if err != nil {
			results.Results[i].Error = apiservererrors.ServerError(err)
			continue
		}
		results.Results[i].StringsWatcherId = id
	}
	return results, nil
}

func (f *Facade) watchApplicationTrustHash(tagString string) (string, error) {
	tag, err := names.ParseApplicationTag(tagString)
	if err != nil {
		return "", errors.Trace(err)
	}
	app, err := f.state.Application(tag.Id())
	if err != nil {
		return "", errors.Trace(err)
	}
	// This is currently implemented by just watching the
	// app config settings which is where the trust value
	// is stored. A similar pattern is used for model config
	// watchers pending better filtering on watchers.
	w := app.WatchConfigSettingsHash()
	if _, ok := <-w.Changes(); ok {
		return f.resources.Register(w), nil
	}
	return "", statewatcher.EnsureErr(w)
}

// UpdateApplicationsService updates the Juju data model to reflect the given
// service details of the specified application.
func (f *Facade) UpdateApplicationsService(ctx context.Context, args params.UpdateApplicationServiceArgs) (params.ErrorResults, error) {
	result := params.ErrorResults{
		Results: make([]params.ErrorResult, len(args.Args)),
	}
	if len(args.Args) == 0 {
		return result, nil
	}
	allSpaces, err := f.networkService.GetAllSpaces(ctx)
	if err != nil {
		return result, apiservererrors.ServerError(err)
	}
	for i, appUpdate := range args.Args {
		appTag, err := names.ParseApplicationTag(appUpdate.ApplicationTag)
		if err != nil {
			result.Results[i].Error = apiservererrors.ServerError(err)
			continue
		}
		pas := params.ToProviderAddresses(appUpdate.Addresses...)
		sAddrs, err := pas.ToSpaceAddresses(allSpaces)
		if err != nil {
			result.Results[i].Error = apiservererrors.ServerError(err)
			continue
		}

		appName := appTag.Id()
		if err := f.applicationService.UpdateCloudService(ctx, appName, appUpdate.ProviderId, sAddrs); err != nil {
			if errors.Is(err, applicationerrors.ApplicationNotFound) {
				err = errors.NotFoundf("application %s not found", appName)
			}
			result.Results[i].Error = apiservererrors.ServerError(err)
		}
		if appUpdate.Scale != nil {
			if err := f.applicationService.SetApplicationScale(ctx, appName, *appUpdate.Scale); err != nil {
				if errors.Is(err, applicationerrors.ApplicationNotFound) {
					err = errors.NotFoundf("application %s not found", appName)
				}
				result.Results[i].Error = apiservererrors.ServerError(err)
			}
		}
	}
	return result, nil
}
