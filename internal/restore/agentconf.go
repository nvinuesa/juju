// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package restore

import (
	"github.com/juju/names/v6"

	"github.com/juju/juju/agent"
	"github.com/juju/juju/controller"
	"github.com/juju/juju/internal/errors"
)

// regenerateAgentConfig rewrites the single-node agent configuration on disk
// so the stopped machine agent boots with the restored source identity. It
// reads <dataDir>/agents/machine-0/agent.conf, changes the controller and
// model tags and the CA controller-agent material (CA cert/key, controller
// cert/key, system identity) to the source values, and writes it back.
// Target-local facts (API addresses, database agent password, API port,
// logging, telemetry) are preserved.
func (r *Restorer) regenerateAgentConfig(archive *Archive) error {
	tag := names.NewMachineTag("0")
	confPath := agent.ConfigPath(r.DataDir, tag)

	conf, err := agent.ReadConfig(confPath)
	if err != nil {
		return errors.Errorf("reading agent config %q: %w: %v", confPath, ErrFinalization, err)
	}

	if len(archive.Controller.Controller) == 0 {
		return errors.Errorf("controller payload has no controller row: %w", ErrFinalization)
	}
	src := archive.Controller.Controller[0]

	conf.SetControllerTag(names.NewControllerTag(archive.ControllerUUID))
	conf.SetModelTag(names.NewModelTag(archive.ControllerModelUUID))
	if src.CaCert != nil {
		conf.SetCACert(*src.CaCert)
	}

	// The API port is a target-local fact preserved from the old config.
	apiPort := 17070
	if info, ok := conf.ControllerAgentInfo(); ok && info.APIPort != 0 {
		apiPort = info.APIPort
	}

	info := controller.ControllerAgentInfo{
		APIPort: apiPort,
	}
	if src.Cert != nil {
		info.Cert = *src.Cert
	}
	if src.PrivateKey != nil {
		info.PrivateKey = *src.PrivateKey
	}
	if src.CaPrivateKey != nil {
		info.CAPrivateKey = *src.CaPrivateKey
	}
	if src.SystemIdentity != nil {
		info.SystemIdentity = *src.SystemIdentity
	}
	conf.SetControllerAgentInfo(info)

	if err := conf.Write(); err != nil {
		return errors.Errorf("writing agent config: %w: %v", ErrFinalization, err)
	}

	// The system identity (machine SSH key) is stored outside agent.conf in
	// its own path; it also gets the source value.
	if err := agent.WriteSystemIdentityFile(conf); err != nil {
		return errors.Errorf("writing system identity file: %w: %v", ErrFinalization, err)
	}
	return nil
}
