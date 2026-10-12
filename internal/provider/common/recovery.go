// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package common

import (
	"context"

	"github.com/juju/names/v6"

	"github.com/juju/juju/core/instance"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/internal/errors"
)

// ProvisionRecoveryMachine wraps an instance-only provisioning operation with
// ownership tracking. Providers retain their finaliser and session semantics;
// failure cleanup is restricted to the instance returned by that operation.
func ProvisionRecoveryMachine(ctx environs.BootstrapContext, env environs.Environ,
	params environs.RecoveryMachineParams,
	provision func(environs.BootstrapContext, environs.BootstrapParams) (*environs.BootstrapResult, error),
) (*environs.BootstrapResult, func(context.Context) error, error) {
	if !names.IsValidMachine(params.MachineID) || params.MachineID == "0" {
		return nil, nil, errors.New("recovery requires a distinct physical machine identity")
	}
	args := params.BootstrapParams
	args.RecoveryMachineID = params.MachineID
	var created instance.Id
	args.InstanceStarted = func(id instance.Id) {
		created = id
		if params.InstanceStarted != nil {
			params.InstanceStarted(id)
		}
	}
	cleanup := func(ctx context.Context) error {
		if created == "" {
			return nil
		}
		return env.StopInstances(ctx, created)
	}
	result, err := provision(ctx, args)
	return result, cleanup, err
}
