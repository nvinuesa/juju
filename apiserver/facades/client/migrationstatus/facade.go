// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package migrationstatus

import (
	"context"

	"github.com/juju/errors"
	"github.com/juju/names/v6"

	apiservererrors "github.com/juju/juju/apiserver/errors"
	"github.com/juju/juju/apiserver/facade"
	coremigration "github.com/juju/juju/core/migration"
	"github.com/juju/juju/core/permission"
	"github.com/juju/juju/rpc/params"
)

// API implements the MigrationStatus facade, a read-only client-facing
// facade that returns credential-free migration status to CLI users.
type API struct {
	authorizer            facade.Authorizer
	controllerUUID        string
	modelMigrationService ModelMigrationService
}

// NewAPI creates a new MigrationStatus facade API.
func NewAPI(
	authorizer facade.Authorizer,
	controllerUUID string,
	modelMigrationService ModelMigrationService,
) (*API, error) {
	if !authorizer.AuthClient() {
		return nil, apiservererrors.ErrPerm
	}
	return &API{
		authorizer:            authorizer,
		controllerUUID:        controllerUUID,
		modelMigrationService: modelMigrationService,
	}, nil
}

// MigrationStatus returns the current status of the latest migration for
// the specified model. The result does not include target controller
// credentials, addresses, or CA certificates.
func (a *API) MigrationStatus(ctx context.Context, args params.ModelArgs) (params.MigrationStatusResult, error) {
	empty := params.MigrationStatusResult{}

	modelTag, err := names.ParseModelTag(args.ModelTag)
	if err != nil {
		return empty, errors.Trace(err)
	}

	// Only controller superusers can check migration status, consistent
	// with InitiateMigration on the Controller facade.
	controllerTag := names.NewControllerTag(a.controllerUUID)
	if err := a.authorizer.HasPermission(ctx, permission.SuperuserAccess, controllerTag); err != nil {
		return empty, errors.Trace(err)
	}

	status, err := a.modelMigrationService.MigrationStatusForModel(ctx, modelTag.Id())
	if err != nil {
		return empty, errors.Trace(err)
	}

	if status.Phase == coremigration.NONE {
		return empty, errors.NotFoundf("migration for model %q", modelTag.Id())
	}

	return params.MigrationStatusResult{
		MigrationId:           status.MigrationUUID,
		Phase:                 status.Phase.String(),
		PhaseChangedTime:      status.PhaseChangedTime,
		StartTime:             status.StartTime,
		StatusMessage:         status.StatusMessage,
		StatusMessageTime:     status.StatusMessageTime,
		TargetControllerUUID:  status.TargetControllerUUID,
		TargetControllerAlias: status.TargetControllerAlias,
	}, nil
}
