// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package commands

import (
	"context"
	"maps"
	"reflect"

	"github.com/juju/errors"

	"github.com/juju/juju/caas"
	"github.com/juju/juju/cmd/cmd"
	"github.com/juju/juju/domain/recovery"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/environs/cloudspec"
	"github.com/juju/juju/environs/config"
	internalrecovery "github.com/juju/juju/internal/recovery"
)

// checkRecoveryModels checks each workload against its own cluster before any
// target resources or local controller registration are created. It returns the
// selected authentication keyed by archived credential identity for import.
func (c *recoveryCommand) checkRecoveryModels(ctx *cmd.Context, deadline context.Context,
	report *environs.RecoverySubstrateReport, controllerCredentials bootstrapCredentials,
) (map[string]internalrecovery.CredentialPatch, error) {
	patches := make(map[string]internalrecovery.CredentialPatch)
	info := c.recoveryInfo
	if info.Credential != nil && controllerCredentials.credential != nil {
		patches[info.Credential.UUID] = internalrecovery.CredentialPatch{
			AuthType:   string(controllerCredentials.credential.AuthType()),
			Attributes: controllerCredentials.credential.Attributes(),
		}
	}
	commandContext := *ctx
	commandContext.Context = deadline
	for _, model := range info.Models {
		if model.UUID == info.ControllerModelUUID {
			continue
		}
		provider, err := environs.Provider(model.CloudType)
		if err != nil {
			return nil, err
		}
		sourceCloud := recoveryCloud(&recovery.ArchiveInfo{Cloud: model.Cloud})
		credentials, region, err := c.recoveryCredentialsForCloud(&commandContext, provider,
			sourceCloud, model.Region, model.Credential)
		if err != nil {
			return nil, errors.Annotatef(err, "resolving credentials for model %q", model.Name)
		}
		spec, err := cloudspec.MakeCloudSpec(sourceCloud, region, credentials.credential)
		if err != nil {
			return nil, err
		}
		if err := provider.ValidateCloud(deadline, spec); err != nil {
			return nil, err
		}
		attrs := make(map[string]any, len(model.Config)+3)
		for key, value := range model.Config {
			attrs[key] = value
		}
		attrs[config.UUIDKey], attrs["name"], attrs["type"] = model.UUID, model.Name, model.CloudType
		cfg, err := config.New(config.UseDefaults, attrs)
		if err != nil {
			return nil, err
		}
		if model.ModelType == "iaas" {
			if _, err := environs.Open(deadline, provider, environs.OpenParams{ControllerUUID: info.ControllerUUID, Cloud: spec, Config: cfg}, environs.NoopCredentialInvalidator()); err != nil {
				return nil, err
			}
		} else {
			broker, err := caas.Open(deadline, provider, environs.OpenParams{
				ControllerUUID: info.ControllerUUID, Cloud: spec, Config: cfg,
			}, environs.NoopCredentialInvalidator())
			if err != nil {
				return nil, err
			}
			checker, ok := broker.(environs.RecoverySubstrateChecker)
			if !ok {
				return nil, errors.NotSupportedf("recovery substrate validation for model %q", model.Name)
			}
			models := recoveryModels(&recovery.ArchiveInfo{Models: []recovery.ModelInfo{model}})
			checked, err := checker.CheckRecoverySubstrate(deadline, environs.RecoverySubstrateParams{
				ControllerUUID: info.ControllerUUID, ControllerModelUUID: info.ControllerModelUUID,
				Models: models, WorkloadOnly: true,
			})
			if err != nil {
				return nil, errors.Annotatef(err, "checking model %q on cloud %q", model.Name, model.CloudName)
			}
			report.MissingWorkloads = append(report.MissingWorkloads, checked.MissingWorkloads...)
			report.MissingPersistentVolumeClaims = append(report.MissingPersistentVolumeClaims, checked.MissingPersistentVolumeClaims...)
		}
		if model.CredentialUUID == "" || credentials.credential == nil {
			continue
		}
		patch := internalrecovery.CredentialPatch{AuthType: string(credentials.credential.AuthType()), Attributes: maps.Clone(credentials.credential.Attributes())}
		if previous, found := patches[model.CredentialUUID]; found && !reflect.DeepEqual(previous, patch) {
			return nil, errors.New("models sharing an archived credential selected different replacement authentication")
		}
		patches[model.CredentialUUID] = patch
	}
	return patches, nil
}
