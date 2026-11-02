// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package commands

import (
	"github.com/juju/juju/controller"
	"github.com/juju/juju/domain/recovery"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/environs/bootstrap"
	"github.com/juju/juju/environs/config"
)

func recoveryConfigs(info *recovery.ArchiveInfo, provider environs.EnvironProvider) (controller.Config, *config.Config, error) {
	fields, _, err := controller.ConfigSchema.ValidationSchema()
	if err != nil {
		return nil, nil, err
	}
	ctrlAttrs := make(map[string]any)
	for key, value := range info.ControllerConfig {
		if field, ok := fields[key]; ok {
			coerced, err := field.Coerce(value, []string{key})
			if err != nil {
				return nil, nil, err
			}
			ctrlAttrs[key] = coerced
		}
	}
	ctrl, err := controller.NewConfig(info.ControllerUUID, info.CACert, ctrlAttrs)
	if err != nil {
		return nil, nil, err
	}
	attrs := make(map[string]any)
	for key, value := range info.ModelConfig {
		attrs[key] = value
	}
	attrs[config.UUIDKey] = info.ControllerModelUUID
	attrs["name"] = "controller"
	attrs["type"] = info.CloudType
	attrs[config.AgentVersionKey] = info.AgentVersion.String()
	// Model config coercion accepts persisted string representations; provider
	// fields are validated when opening the provider.
	modelConfig, err := config.New(config.UseDefaults, attrs)
	return ctrl, modelConfig, err
}

func recoveryCertificate(info *recovery.ArchiveInfo, cfg controller.Config) (controller.ControllerAgentInfo, error) {
	agentInfo, err := bootstrap.ControllerAgentInfoForCA(info.CACert, info.CAPrivateKey, cfg.APIPort())
	if err != nil {
		return controller.ControllerAgentInfo{}, err
	}
	agentInfo.SystemIdentity = info.SystemIdentity
	return agentInfo, nil
}
