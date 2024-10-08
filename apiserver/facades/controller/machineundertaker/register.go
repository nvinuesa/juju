// Copyright 2022 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package machineundertaker

import (
	"context"
	"reflect"

	"github.com/juju/juju/apiserver/facade"
)

// Register is called to expose a package of facades onto a given registry.
func Register(registry facade.FacadeRegistry) {
	registry.MustRegister("MachineUndertaker", 1, func(stdCtx context.Context, ctx facade.ModelContext) (facade.Facade, error) {
		return newFacade(ctx)
	}, reflect.TypeOf((*API)(nil)))
}

// newFacade provides the signature required for facade registration.
func newFacade(ctx facade.ModelContext) (*API, error) {
	return NewAPI(
		ctx.ModelUUID(),
		&backendShim{State: ctx.State()},
		ctx.Resources(),
		ctx.Auth(),
		ctx.DomainServices().Machine(),
	)
}
