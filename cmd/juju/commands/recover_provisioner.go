// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package commands

import (
	"context"
	"encoding/binary"
	"maps"
	"sort"
	"strconv"
	"time"

	"github.com/juju/errors"
	"github.com/juju/utils/v4/ssh"

	"github.com/juju/juju/api/jujuclient"
	"github.com/juju/juju/cloud"
	"github.com/juju/juju/cmd/cmd"
	"github.com/juju/juju/controller"
	"github.com/juju/juju/core/base"
	"github.com/juju/juju/core/model"
	"github.com/juju/juju/core/permission"
	"github.com/juju/juju/domain/recovery"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/environs/bootstrap"
	"github.com/juju/juju/environs/cloudspec"
	environscmd "github.com/juju/juju/environs/cmd"
	"github.com/juju/juju/environs/config"
	"github.com/juju/juju/internal/password"
	internalrecovery "github.com/juju/juju/internal/recovery"
	jujussh "github.com/juju/juju/internal/ssh"
	"github.com/juju/juju/internal/storage"
	"github.com/juju/juju/internal/tools"
	"github.com/juju/juju/internal/uuid"
)

// provisionRecoveryController owns replacement provisioning. It deliberately
// does not call the fresh controller bootstrap orchestrator.
func (c *recoveryCommand) provisionRecoveryController(ctx *cmd.Context) (resultErr error) {
	info := c.recoveryInfo
	if info.CloudType == cloud.CloudTypeKubernetes {
		return c.provisionK8sRecoveryController(ctx)
	}
	bundle, err := internalrecovery.SelectAgent(ctx, ctx.AbsPath(c.archivePath), info)
	if err != nil {
		return errors.Annotate(err, "selecting archived controller agent")
	}
	defer bundle.Close()
	sourceBase, err := base.ParseBaseFromString(info.SourceBase)
	if err != nil {
		return errors.Annotate(err, "archive does not record a usable controller base")
	}
	provider, err := environs.Provider(info.CloudType)
	if err != nil {
		return errors.Trace(err)
	}
	sourceCloud := recoveryCloud(info)
	credentials, region, err := c.recoveryCredentials(ctx, provider, sourceCloud)
	if err != nil {
		return err
	}
	spec, err := cloudspec.MakeCloudSpec(sourceCloud, region, credentials.credential)
	if err != nil {
		return err
	}
	spec.IsControllerCloud = true
	controllerConfig, modelConfig, err := recoveryConfigs(info, provider)
	if err != nil {
		return errors.Annotate(err, "building recovery node configuration")
	}
	_, err = recoveryCertificate(info, controllerConfig)
	if err != nil {
		return err
	}
	if err := provider.ValidateCloud(ctx, spec); err != nil {
		return err
	}
	env, err := environs.Open(ctx, provider, environs.OpenParams{ControllerUUID: info.ControllerUUID, Cloud: spec, Config: modelConfig}, environs.NoopCredentialInvalidator())
	if err != nil {
		return err
	}
	adminPassword, err := password.RandomPassword()
	if err != nil {
		return err
	}
	stdCtx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()
	provisionCtx := environscmd.BootstrapContext(stdCtx, ctx)
	var cleanup func(context.Context) error
	registered := false
	defer func() {
		resultErr = c.cleanupFailedRecovery(ctx, cleanup, registered, resultErr)
	}()
	provisioner, ok := env.(environs.RecoveryMachineProvisioner)
	if !ok {
		return errors.NotSupportedf("machine recovery provisioning for %q", info.CloudType)
	}
	if _, err := provisioner.PrepareForRecovery(provisionCtx, environs.RecoverySubstrateParams{ControllerUUID: info.ControllerUUID, ControllerName: info.ControllerName, ControllerModelUUID: info.ControllerModelUUID}); err != nil {
		return err
	}
	_, err = c.checkRecoveryModels(ctx, stdCtx, &environs.RecoverySubstrateReport{}, credentials)
	if err != nil {
		return err
	}
	attempt, err := uuid.NewUUID()
	if err != nil {
		return err
	}
	physicalID := strconv.FormatUint(binary.BigEndian.Uint64(attempt[:8])&((1<<63)-1), 10)
	if physicalID == "0" || physicalID == info.MachineName {
		return errors.New("replacement provisioning identity collides with the source")
	}

	keyFiles := ssh.PrivateKeyFiles()
	if len(keyFiles) == 0 {
		return errors.New("no Juju SSH keys available for recovery")
	}
	sort.Strings(keyFiles)
	keys, err := jujussh.PublicKeysForPrivateKeyFiles(keyFiles)
	if err != nil {
		return err
	}
	sourceConstraints := info.Constraints
	if sourceConstraints.Arch != nil && *sourceConstraints.Arch != bundle.Tools.Version.Arch {
		return errors.New("archived agent architecture differs from source constraints")
	}
	sourceConstraints.Arch = &bundle.Tools.Version.Arch
	cons, err := internalrecovery.SizeRecoveryDisk(info, sourceConstraints)
	if err != nil {
		return err
	}
	pools := make(map[string]storage.Attrs, len(info.StoragePools))
	for name, recorded := range info.StoragePools {
		attrs := make(storage.Attrs, len(recorded))
		for key, value := range recorded {
			attrs[key] = value
		}
		pools[name] = attrs
	}
	params, err := bootstrap.PrepareRecoveryMachine(provisionCtx, env, environs.BootstrapParams{
		StoragePools: pools,
		CloudName:    info.CloudName, CloudRegion: info.Region,
		ControllerConfig: controllerConfig, AuthorizedKeys: keys,
		BootstrapConstraints: cons, AvailableTools: tools.List{bundle.Tools},
		BootstrapBase: sourceBase, SupportedBootstrapBases: []base.Base{sourceBase},
	})
	if err != nil {
		return err
	}
	registered, err = c.registerRecoveryController(info, controllerConfig, env.Config(), spec, credentials.name, adminPassword, model.IAAS)
	if err != nil {
		return err
	}
	_, recoveryCleanup, err := provisioner.ProvisionRecoveryMachine(provisionCtx, environs.RecoveryMachineParams{BootstrapParams: params, MachineID: physicalID})
	cleanup = recoveryCleanup
	if err != nil {
		return err
	}
	return errors.New("controller recovery initialisation is not available in this build")
}

// cleanupFailedRecovery leaves successful recoveries untouched. On failure,
// only --keep-broken preserves cloud resources; failed client registrations
// are always removed.
func (c *recoveryCommand) cleanupFailedRecovery(
	ctx *cmd.Context,
	cleanup func(context.Context) error,
	registered bool,
	recoveryErr error,
) error {
	if recoveryErr == nil {
		return nil
	}
	if cleanup != nil {
		if c.keepBroken {
			ctx.Infof(`
Recovery failed but --keep-broken was specified.
Any replacement resources for controller %q in cloud %q have been retained.
Access them directly using SSH or your cloud tools for diagnosis.
Stop recovery and normal controller processes before starting safe mode.
An incomplete import cannot be resumed automatically.
Remove retained resources manually when finished.
`[1:], c.recoveryInfo.ControllerName, c.recoveryInfo.CloudName)
		} else {
			// Cleanup must still work after the recovery context times out.
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
			defer cancel()
			if err := cleanup(cleanupCtx); err != nil {
				recoveryErr = errors.Annotatef(recoveryErr, "cleanup of replacement resources failed: %v", err)
			}
		}
	}
	if registered {
		if err := c.ClientStore().RemoveController(c.recoveryInfo.ControllerName); err != nil {
			recoveryErr = errors.Annotatef(recoveryErr, "removing replacement registration: %v", err)
		}
	}
	return recoveryErr
}

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

func (c *recoveryCommand) registerRecoveryController(info *recovery.ArchiveInfo, cfg controller.Config, modelConfig *config.Config, spec cloudspec.CloudSpec, credentialName, adminPassword string, modelType model.ModelType) (bool, error) {
	store := c.ClientStore()
	if err := store.AddController(info.ControllerName, jujuclient.ControllerDetails{ControllerUUID: info.ControllerUUID, CACert: info.CACert,
		Cloud: spec.Name, CloudType: spec.Type, CloudRegion: spec.Region, AgentVersion: info.AgentVersion.String()}); err != nil {
		return false, err
	}
	if err := store.UpdateAccount(info.ControllerName, jujuclient.AccountDetails{User: "admin", Password: adminPassword, LastKnownAccess: string(permission.SuperuserAccess)}); err != nil {
		return true, err
	}
	if err := store.UpdateModel(info.ControllerName, "admin/controller", jujuclient.ModelDetails{ModelUUID: info.ControllerModelUUID, ModelType: modelType}); err != nil {
		return true, err
	}
	if err := store.SetCurrentModel(info.ControllerName, "admin/controller"); err != nil {
		return true, err
	}
	attrs := modelConfig.AllAttrs()
	delete(attrs, config.UUIDKey)
	publicControllerConfig := maps.Clone(cfg)
	delete(publicControllerConfig, controller.CACertKey)
	delete(publicControllerConfig, controller.ControllerUUIDKey)
	return true, store.UpdateBootstrapConfig(info.ControllerName, jujuclient.BootstrapConfig{ControllerConfig: publicControllerConfig, Config: attrs, ControllerModelUUID: info.ControllerModelUUID,
		Cloud: spec.Name, CloudType: spec.Type, CloudRegion: spec.Region, CloudEndpoint: spec.Endpoint, CloudIdentityEndpoint: spec.IdentityEndpoint, CloudStorageEndpoint: spec.StorageEndpoint,
		CloudCACertificates: spec.CACertificates, SkipTLSVerify: spec.SkipTLSVerify, Credential: credentialName})
}
