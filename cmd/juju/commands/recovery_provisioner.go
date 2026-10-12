// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package commands

import (
	"context"
	"encoding/binary"
	"maps"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/juju/errors"
	"github.com/juju/names/v6"
	"github.com/juju/utils/v4/ssh"

	"github.com/juju/juju/api"
	"github.com/juju/juju/api/jujuclient"
	"github.com/juju/juju/cloud"
	"github.com/juju/juju/cmd/cmd"
	"github.com/juju/juju/cmd/juju/common"
	"github.com/juju/juju/cmd/modelcmd"
	"github.com/juju/juju/controller"
	"github.com/juju/juju/core/base"
	"github.com/juju/juju/core/constraints"
	"github.com/juju/juju/core/instance"
	"github.com/juju/juju/core/model"
	"github.com/juju/juju/core/network"
	"github.com/juju/juju/core/permission"
	"github.com/juju/juju/domain/recovery"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/environs/bootstrap"
	"github.com/juju/juju/environs/cloudspec"
	environscmd "github.com/juju/juju/environs/cmd"
	"github.com/juju/juju/environs/config"
	"github.com/juju/juju/internal/cloudconfig/instancecfg"
	"github.com/juju/juju/internal/password"
	internalrecovery "github.com/juju/juju/internal/recovery"
	jujussh "github.com/juju/juju/internal/ssh"
	"github.com/juju/juju/internal/storage"
	"github.com/juju/juju/internal/tools"
	"github.com/juju/juju/internal/uuid"
	jujunames "github.com/juju/juju/juju/names"
)

// provisionRecoveryController owns replacement provisioning. It deliberately
// does not call the fresh controller bootstrap orchestrator.
func (c *recoveryCommand) provisionRecoveryController(ctx *cmd.Context) (resultErr error) {
	info := c.recoveryInfo
	if info.CloudType == cloud.CloudTypeKubernetes {
		return errors.New("Kubernetes recovery requires the Kubernetes recovery implementation")
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
	certInfo, err := recoveryCertificate(info, controllerConfig)
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
	var started instance.Id
	var cleanup func(context.Context) error
	registered := false
	defer func() {
		if resultErr == nil {
			return
		}
		// Cleanup must work after timeout and is restricted to this instance.
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
		defer cancel()
		if cleanup != nil {
			if err := cleanup(cleanupCtx); err != nil {
				resultErr = errors.Annotatef(resultErr, "cleanup of replacement %s failed: %v", started, err)
			}
		}
		if registered {
			if err := c.ClientStore().RemoveController(info.ControllerName); err != nil {
				resultErr = errors.Annotatef(resultErr, "removing replacement registration: %v", err)
			}
		}
	}()
	provisioner, ok := env.(environs.RecoveryMachineProvisioner)
	if !ok {
		return errors.NotSupportedf("machine recovery provisioning for %q", info.CloudType)
	}
	if _, err := provisioner.PrepareForRecovery(provisionCtx, environs.RecoverySubstrateParams{ControllerUUID: info.ControllerUUID, ControllerName: info.ControllerName, ControllerModelUUID: info.ControllerModelUUID}); err != nil {
		return err
	}
	credentialPatches, err := c.checkRecoveryModels(ctx, stdCtx, &environs.RecoverySubstrateReport{}, credentials)
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
		InstanceStarted: func(id instance.Id) { started = id },
	})
	if err != nil {
		return err
	}
	registered, err = c.registerRecoveryController(info, controllerConfig, env.Config(), spec, credentials.name, adminPassword, model.IAAS)
	if err != nil {
		return err
	}
	result, recoveryCleanup, err := provisioner.ProvisionRecoveryMachine(provisionCtx, environs.RecoveryMachineParams{BootstrapParams: params, MachineID: physicalID})
	cleanup = recoveryCleanup
	if err != nil {
		return err
	}
	icfg, err := instancecfg.NewBootstrapInstanceConfig(controllerConfig, params.BootstrapConstraints, constraints.Value{}, result.Base, "", nil)
	if err != nil {
		return err
	}
	// Physical resource names belong to this attempt. The installed agent
	// retains the archived logical machine identity.
	icfg.MachineId = info.MachineName
	icfg.MachineAgentServiceName = jujunames.JujuAgentd + "-" + names.NewMachineTag(info.MachineName).String()
	if result.Base != sourceBase {
		return errors.New("replacement base differs from archived controller")
	}
	if result.Arch != bundle.Tools.Version.Arch {
		return errors.New("replacement architecture differs from archived agent")
	}
	if err := icfg.SetTools(tools.List{bundle.Tools}); err != nil {
		return err
	}
	icfg.APIInfo = &api.Info{Password: adminPassword, CACert: info.CACert, ModelTag: names.NewModelTag(info.ControllerModelUUID)}
	icfg.Bootstrap.ControllerAgentInfo = certInfo
	icfg.Bootstrap.ControllerCloud = sourceCloud
	icfg.Bootstrap.ControllerCloudRegion = region
	icfg.Bootstrap.ControllerCloudCredential = credentials.credential
	icfg.Bootstrap.ControllerCloudCredentialName = credentials.name
	icfg.Bootstrap.ControllerModelConfig = env.Config()
	icfg.Bootstrap.ControllerConfig = controllerConfig
	icfg.Bootstrap.AgentVersion = info.AgentVersion
	icfg.Bootstrap.BootstrapSSHAuthorizedKeys = keys
	icfg.Bootstrap.ControllerModelAuthorizedKeys, err = jujussh.SplitAuthorizedKeys(env.Config().AuthorizedKeys())
	if err != nil {
		return err
	}
	icfg.Initialisation = &instancecfg.ControllerInitialisation{
		Command: "recovery-state", ParamsPath: internalrecovery.ParamsFile,
		ModePath: internalrecovery.ModeFile, Mode: "true", Timeout: 20 * time.Minute,
		Prepare: func(cfg *instancecfg.InstanceConfig) ([]byte, error) {
			return (internalrecovery.Params{Credentials: credentialPatches, ArchivePath: filepath.Join(cfg.DataDir, "recovery", "archive.tar.gz"), SHA256: info.Checksum, AgentVersion: info.AgentVersion,
				MachineName: info.MachineName, MachineNonce: cfg.MachineNonce, Node: cfg.Bootstrap.StateInitializationParams}).Marshal()
		},
		Files: []instancecfg.InitialisationFile{{Source: ctx.AbsPath(c.archivePath), Destination: filepath.Join(icfg.DataDir, "recovery", "archive.tar.gz"), SHA256: info.Checksum}},
	}
	if result.CloudBootstrapFinalizer == nil {
		return errors.New("provider returned no machine controller finaliser")
	}
	if err := result.CloudBootstrapFinalizer(provisionCtx, icfg, environs.BootstrapDialOpts{IdentityFiles: keyFiles, Timeout: 20 * time.Minute, RetryDelay: 5 * time.Second, AddressesDelay: 5 * time.Second}); err != nil {
		return err
	}
	addresses, err := recoveryInstanceAddresses(ctx, env, started)
	if err != nil {
		return err
	}
	if err := saveControllerConnection(provisionCtx, c.ClientStore(), info.ControllerName, env, controllerConfig, info.AgentVersion.String(), addresses); err != nil {
		return err
	}
	if err := c.SetModelIdentifier(modelcmd.JoinModelName(info.ControllerName, "admin/controller"), false); err != nil {
		return err
	}
	if err := waitForAgentInitialisation(provisionCtx, &c.ModelCommandBase, false, info.ControllerName, common.TryAPI); err != nil {
		return err
	}
	c.printRecoverySummary(ctx, info, nil)
	return nil
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

func recoveryInstanceAddresses(ctx context.Context, env environs.InstanceLister, id instance.Id) (network.ProviderAddresses, error) {
	instances, err := env.Instances(ctx, []instance.Id{id})
	if err != nil {
		return nil, err
	}
	if len(instances) != 1 || instances[0] == nil || instances[0].Id() != id {
		return nil, errors.NotFoundf("replacement instance %s", id)
	}
	return instances[0].Addresses(ctx)
}
