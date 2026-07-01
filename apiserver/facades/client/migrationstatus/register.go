// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package migrationstatus

import (
	"context"
	"reflect"

	"github.com/juju/errors"

	"github.com/juju/juju/apiserver/facade"
)

// Register is called to expose a package of facades onto a given registry.
func Register(registry facade.FacadeRegistry) {
	registry.MustRegisterForMultiModel("MigrationStatus", 1, func(stdCtx context.Context, ctx facade.MultiModelContext) (facade.Facade, error) {
		api, err := newMigrationStatusAPI(stdCtx, ctx)
		if err != nil {
			return nil, errors.Trace(err)
		}
		return api, nil
	}, reflect.TypeFor[*API]())
}

func newMigrationStatusAPI(stdCtx context.Context, ctx facade.MultiModelContext) (*API, error) {
	domainServices := ctx.DomainServices()
	return NewAPI(
		ctx.Auth(),
		ctx.ControllerUUID(),
		domainServices.ModelMigration(),
	)
}
