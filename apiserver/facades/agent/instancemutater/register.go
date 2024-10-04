// Copyright 2022 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package instancemutater

import (
	"context"
	"reflect"

	"github.com/juju/juju/apiserver/facade"
)

// Register is called to expose a package of facades onto a given registry.
func Register(registry facade.FacadeRegistry) {
	registry.MustRegister("InstanceMutater", 3, func(stdCtx context.Context, ctx facade.ModelContext) (facade.Facade, error) {
		return newFacadeV3(ctx)
	}, reflect.TypeOf((*InstanceMutaterAPI)(nil)))
}

// newFacadeV3 is used for API registration.
func newFacadeV3(ctx facade.ModelContext) (*InstanceMutaterAPI, error) {
	st := &instanceMutaterStateShim{State: ctx.State()}

	machineService := ctx.DomainServices().Machine()
	watcher := &instanceMutatorWatcher{
		st:             st,
		machineService: machineService,
	}
	return NewInstanceMutaterAPI(
		st,
		machineService,
		watcher,
		ctx.Resources(),
		ctx.Auth(),
		ctx.Logger().Child("instancemutater"),
	)
}
