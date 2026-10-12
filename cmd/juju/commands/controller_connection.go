// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package commands

import (
	"context"

	"github.com/juju/errors"

	"github.com/juju/juju/api/jujuclient"
	"github.com/juju/juju/controller"
	"github.com/juju/juju/core/network"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/internal/proxy"
	"github.com/juju/juju/juju"
)

// saveControllerConnection records the connection to a newly provisioned
// controller. Recovery and fresh bootstrap must persist the same proxy and
// endpoint details before attempting their first API connection.
func saveControllerConnection(ctx context.Context, store jujuclient.ClientStore,
	name string, provider environs.BootstrapEnviron, cfg controller.Config,
	agentVersion string, addresses []network.ProviderAddress) error {
	var proxier proxy.Proxier
	if connector, ok := provider.(environs.ConnectorInfo); ok {
		var err error
		proxier, err = connector.ConnectionProxyInfo(ctx)
		if err != nil && !errors.Is(err, errors.NotFound) {
			return errors.Trace(err)
		}
	}
	hostPorts := make(network.MachineHostPorts, len(addresses))
	for i, address := range addresses {
		hostPorts[i] = network.MachineHostPort{
			MachineAddress: address.MachineAddress,
			NetPort:        network.NetPort(cfg.APIPort()),
		}
	}
	return errors.Annotate(juju.UpdateControllerDetailsFromLogin(store, name,
		juju.UpdateControllerParams{
			AgentVersion:     agentVersion,
			CurrentHostPorts: []network.MachineHostPorts{hostPorts},
			PublicDNSName:    newStringIfNonEmpty(cfg.AutocertDNSName()),
			MachineCount:     new(1), ControllerMachineCount: new(1),
			Proxier: proxier,
		}), "saving controller connection")
}
