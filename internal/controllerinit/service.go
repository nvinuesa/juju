// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package controllerinit

import (
	"context"

	"github.com/juju/juju/controller"
	coreapplication "github.com/juju/juju/core/application"
	"github.com/juju/juju/core/machine"
	"github.com/juju/juju/core/network"
	"github.com/juju/juju/core/unit"
	"github.com/juju/juju/domain/controllernode"
)

type AgentPasswordService interface {
	// SetApplicationPassword sets the password for the given application.
	SetApplicationPassword(ctx context.Context, appID coreapplication.UUID, password string) error
	// SetUnitPassword sets the password for the given unit.
	SetUnitPassword(ctx context.Context, unitName unit.Name, password string) error
	// SetMachinePassword sets the password for the given machine.
	SetMachinePassword(ctx context.Context, machineName machine.Name, password string) error
	// SetControllerNodePassword sets the password for the controller node.
	SetControllerNodePassword(ctx context.Context, controllerID string, password string) error
	// EnsureControllerNodeNonce returns the persisted introduction nonce for a
	// controller node, creating it from nonce only when it is not already set.
	EnsureControllerNodeNonce(ctx context.Context, controllerID, nonce string) (string, error)
}

type ControllerConfigService interface {
	ControllerConfig(context.Context) (controller.Config, error)
}

type ControllerNodeService interface {
	// SetAPIAddresses sets the provided addresses associated with the provided
	// controller IDs.
	//
	// The following errors can be expected:
	// - [controllernodeerrors.StaleControllerMembership] if controller membership
	// changed before the addresses were published.
	SetAPIAddresses(ctx context.Context, args controllernode.SetAPIAddressArgs) error
}

type NetworkService interface {
	// SpaceByName returns a space from state that matches the input name.
	SpaceByName(ctx context.Context, name network.SpaceName) (*network.SpaceInfo, error)
	// GetAllSpaces returns all spaces for the model.
	GetAllSpaces(ctx context.Context) (network.SpaceInfos, error)
	// ReloadSpaces loads spaces and subnets from the provider into state.
	ReloadSpaces(ctx context.Context) error
}

type ServiceManager interface {
	ControllerUnitFQDN(int) string
}
type ServiceManagerGetterFunc func(context.Context) (ServiceManager, error)
