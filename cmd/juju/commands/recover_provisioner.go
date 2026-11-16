// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package commands

import (
	"context"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"time"

	"github.com/juju/names/v6"
	"github.com/juju/utils/v4/ssh"

	"github.com/juju/juju/api/jujuclient"
	jujucloud "github.com/juju/juju/cloud"
	"github.com/juju/juju/cmd/cmd"
	"github.com/juju/juju/cmd/juju/common"
	"github.com/juju/juju/cmd/modelcmd"
	"github.com/juju/juju/controller"
	"github.com/juju/juju/core/arch"
	"github.com/juju/juju/core/base"
	"github.com/juju/juju/core/constraints"
	coreerrors "github.com/juju/juju/core/errors"
	"github.com/juju/juju/core/semversion"
	jujuversion "github.com/juju/juju/core/version"
	"github.com/juju/juju/domain/recovery"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/environs/bootstrap"
	"github.com/juju/juju/environs/cloudspec"
	environscmd "github.com/juju/juju/environs/cmd"
	"github.com/juju/juju/environs/config"
	"github.com/juju/juju/environs/sync"
	"github.com/juju/juju/internal/errors"
	"github.com/juju/juju/internal/password"
	jujussh "github.com/juju/juju/internal/ssh"
	"github.com/juju/juju/internal/tools"
)

// recoveryAllocationTimeout bounds provider preparation and machine allocation
// after the client has resolved the cloud and credentials.
const recoveryAllocationTimeout = 20 * time.Minute

var lookupRecoveryProvider = environs.Provider

// recoveryK8sBroker is the namespace-bearing surface the recovery
// provisioner needs from a Kubernetes environ.
type recoveryK8sBroker interface {
	Namespace() string
}

// provisionRecovery installs and starts a replacement controller agent
// from the recovery archive, through the ordinary bootstrap allocation
// pipeline, and deliberately stops before initialising its database.
func provisionRecovery(
	ctx *cmd.Context,
	store jujuclient.ClientStore,
	archivePath string,
	info *recovery.ArchiveInfo,
) error {
	isK8s := jujucloud.CloudTypeIsCAAS(info.CloudType)

	var iaasParams bootstrap.IAASRecoveryParams
	if isK8s {
		if info.ControllerName == "" {
			return errors.New("recovery archive does not record the source controller name")
		}
	} else {
		if err := info.CheckAgentVersion(jujuversion.Current); err != nil {
			return errors.Capture(err)
		}
		var err error
		iaasParams, err = recoveryParamsFromArchive(info)
		if err != nil {
			return errors.Capture(err)
		}
		if !names.IsValidMachine(info.MachineName) {
			return errors.New("archive does not record a usable controller machine")
		}
	}

	credentials, err := recoveryCredentials(ctx, store, info)
	if err != nil {
		return errors.Capture(err)
	}

	controllerConfig, modelConfig, err := recoveryConfigs(info)
	if err != nil {
		return errors.Capture(err)
	}

	// Let allocation unwind on Ctrl-C so it can report an instance that
	// may need manual removal.
	interruptCtx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()
	stdCtx, cancel := context.WithTimeout(interruptCtx, recoveryAllocationTimeout)
	defer cancel()
	provisionCtx := environscmd.BootstrapContext(stdCtx, ctx)

	adminPassword, err := password.RandomPassword()
	if err != nil {
		return errors.Capture(err)
	}
	preparation, err := bootstrap.OpenController(isK8s, provisionCtx, bootstrap.PrepareParams{
		ModelConfig:      modelConfig.AllAttrs(),
		ControllerConfig: controllerConfig,
		ControllerName:   info.ControllerName,
		Cloud:            credentials.spec,
		CredentialName:   credentials.credentialName,
		AdminSecret:      adminPassword,
	})
	if err != nil {
		return errors.Capture(err)
	}
	var k8sBroker recoveryK8sBroker
	if isK8s {
		broker, ok := preparation.Environ.(recoveryK8sBroker)
		if !ok {
			return errors.Errorf(
				"Kubernetes recovery provisioning for %T %w", preparation.Environ, coreerrors.NotSupported)
		}
		k8sBroker = broker
	}
	if err := preparation.Environ.PrepareForBootstrap(provisionCtx, info.ControllerName); err != nil {
		return errors.Capture(err)
	}

	commonBootstrap := bootstrap.BootstrapParams{
		ControllerName:      info.ControllerName,
		CAPrivateKey:        info.CAPrivateKey,
		AgentVersion:        &info.AgentVersion,
		Cloud:               credentials.cloud,
		CloudRegion:         credentials.spec.Region,
		CloudCredentialName: credentials.credentialName,
		CloudCredential:     credentials.spec.Credential,
		ControllerConfig:    controllerConfig,
		AdminSecret:         adminPassword,
	}

	if isK8s {
		commonBootstrap.DialOpts.Timeout = 20 * time.Minute
		commonBootstrap.SupportedBootstrapBases = base.ControllerBases()
		k8sParams := bootstrap.K8sRecoveryParams{
			BootstrapParams: commonBootstrap,
		}
		if err := bootstrap.RecoverK8s(provisionCtx, preparation.Environ, k8sParams); err != nil {
			return errors.Capture(err)
		}
		return errors.Errorf(
			"controller recovery database import is not available in this build; started replacement agent in namespace %s",
			k8sBroker.Namespace(),
		)
	}

	keyFiles := ssh.PrivateKeyFiles()
	if len(keyFiles) == 0 {
		return errors.New("no Juju SSH keys available for recovery")
	}
	sort.Strings(keyFiles)
	authorizedKeys, err := jujussh.PublicKeysForPrivateKeyFiles(keyFiles)
	if err != nil {
		return errors.Capture(err)
	}
	builtAgent, err := sync.BuildAgentTarball(
		true, "bootstrap", func(semversion.Number) semversion.Number {
			return info.AgentVersion
		},
	)
	if err != nil {
		return errors.Capture(err)
	}
	defer func() { _ = os.RemoveAll(builtAgent.Dir) }()

	// A locally built agent only runs on the client platform, so
	// refuse to install it on a replacement with a different base
	// or architecture.
	targetArch := *iaasParams.BootstrapConstraints.Arch
	if builtAgent.Version.Arch != targetArch ||
		builtAgent.Version.Release != iaasParams.BootstrapBase.OS {
		return errors.Errorf(
			"cannot use agent built for %q using a client running on %q",
			iaasParams.BootstrapBase, builtAgent.Version,
		)
	}

	commonBootstrap.BootstrapConstraints = iaasParams.BootstrapConstraints
	commonBootstrap.BootstrapBase = iaasParams.BootstrapBase
	commonBootstrap.SupportedBootstrapBases = iaasParams.SupportedBootstrapBases
	commonBootstrap.BootstrapSSHAuthorizedKeys = authorizedKeys
	commonBootstrap.DialOpts.Timeout = 20 * time.Minute
	commonBootstrap.DialOpts.IdentityFiles = keyFiles
	commonBootstrap.DialOpts.RetryDelay = 5 * time.Second
	commonBootstrap.DialOpts.AddressesDelay = 5 * time.Second
	iaasParams.BootstrapParams = commonBootstrap
	iaasParams.ArchivePath = archivePath
	iaasParams.ArchiveSHA256 = info.Checksum
	iaasParams.SystemIdentity = info.SystemIdentity
	iaasParams.MachineId = info.MachineName
	iaasParams.AvailableTools = tools.List{&tools.Tools{
		Version: semversion.Binary{
			Number:  info.AgentVersion,
			Release: iaasParams.BootstrapBase.OS,
			Arch:    targetArch,
		},
		URL:    "file://" + filepath.Join(builtAgent.Dir, builtAgent.StorageName),
		SHA256: builtAgent.Sha256Hash,
		Size:   builtAgent.Size,
	}}
	started, err := bootstrap.RecoverIAAS(provisionCtx, preparation.Environ, iaasParams)
	if err != nil {
		return errors.Capture(err)
	}
	return errors.Errorf(
		"controller recovery database import is not available in this build; initialised replacement database on instance %s",
		started,
	)
}

// recoveryParamsFromArchive prepares the machine-specific inputs before cloud
// access. The client can run on a different platform from the controller.
func recoveryParamsFromArchive(info *recovery.ArchiveInfo) (bootstrap.IAASRecoveryParams, error) {
	if info.SourceBase == "" {
		return bootstrap.IAASRecoveryParams{}, errors.New("archive does not record a usable controller base")
	}
	targetBase, err := base.ParseBaseFromString(info.SourceBase)
	if err != nil {
		return bootstrap.IAASRecoveryParams{}, errors.Errorf("archive does not record a usable controller base: %w", err)
	}
	targetArch := arch.NormaliseArch(info.SourceArch)
	if !arch.IsSupportedArch(targetArch) {
		return bootstrap.IAASRecoveryParams{}, errors.Errorf("archive does not record a usable controller architecture: %q", info.SourceArch)
	}
	return bootstrap.IAASRecoveryParams{
		BootstrapParams: bootstrap.BootstrapParams{
			ControllerName:          info.ControllerName,
			CAPrivateKey:            info.CAPrivateKey,
			AgentVersion:            &info.AgentVersion,
			BootstrapConstraints:    constraints.Value{Arch: &targetArch},
			BootstrapBase:           targetBase,
			SupportedBootstrapBases: []base.Base{targetBase},
		},
		// Allocation selects an image but does not download an agent, so
		// only the binary identity is needed; URL, digest and size are unset.
		// TODO(recovery): supply a verified agent tarball before installation.
		AvailableTools: tools.List{{Version: semversion.Binary{
			Number: info.AgentVersion, Release: targetBase.OS, Arch: targetArch,
		}}},
	}, nil
}

type recoveryTarget struct {
	cloud          jujucloud.Cloud
	spec           cloudspec.CloudSpec
	credentialName string
}

// recoveryCredentials resolves the archived cloud against the local cloud
// definition, selected region and credentials used for allocation.
func recoveryCredentials(ctx *cmd.Context, store jujuclient.ClientStore, info *recovery.ArchiveInfo) (*recoveryTarget, error) {
	isK8s := jujucloud.CloudTypeIsCAAS(info.CloudType)
	cloud, err := common.CloudByName(info.CloudName)
	if err != nil {
		return nil, errors.Capture(err)
	}
	if err := info.CheckProviderFamily(cloud.Type); err != nil {
		return nil, errors.Capture(err)
	}
	if !isK8s && jujucloud.CloudIsCAAS(*cloud) {
		return nil, errors.Errorf("Kubernetes recovery provisioning %w", coreerrors.NotSupported)
	}
	provider, err := lookupRecoveryProvider(info.CloudType)
	if err != nil {
		return nil, errors.Capture(err)
	}
	if finalizer, ok := provider.(environs.CloudFinalizer); ok {
		finalized, err := finalizer.FinalizeCloud(ctx, *cloud)
		if err != nil {
			return nil, errors.Capture(err)
		}
		cloud = &finalized
	}
	if isK8s && cloud.HostCloudRegion == "" {
		return nil, errors.New("recovery cloud does not record a host cloud region; re-import it with juju add-k8s --client")
	}
	credential, name, regionName, _, err := common.GetOrDetectCredential(
		ctx, store, provider, modelcmd.GetCredentialsParams{Cloud: *cloud},
	)
	if err != nil {
		return nil, errors.Capture(err)
	}
	region, err := common.ChooseCloudRegion(*cloud, regionName)
	if err != nil {
		return nil, errors.Capture(err)
	}
	spec, err := cloudspec.MakeCloudSpec(*cloud, region.Name, credential)
	if err != nil {
		return nil, errors.Capture(err)
	}
	spec.IsControllerCloud = true
	return &recoveryTarget{cloud: *cloud, spec: spec, credentialName: name}, nil
}

// recoveryConfigs preserves the archived controller identity and CA so that
// surviving agents can trust the replacement controller.
func recoveryConfigs(info *recovery.ArchiveInfo) (controller.Config, *config.Config, error) {
	controllerConfig, err := controller.NewConfig(
		info.ControllerUUID,
		info.CACert,
		map[string]any{controller.ControllerName: info.ControllerName},
	)
	if err != nil {
		return nil, nil, errors.Capture(err)
	}
	// TODO(recovery): reconcile archived settings instead of relying on
	// allocation defaults when restoring the controller model. The local
	// bootstrap name is conventional; the archived UUID identifies the model.
	modelConfig, err := config.New(config.UseDefaults, map[string]any{
		"name":                 bootstrap.ControllerModelName,
		"type":                 info.CloudType,
		config.UUIDKey:         info.ControllerModelUUID,
		config.AgentVersionKey: info.AgentVersion.String(),
	})
	if err != nil {
		return nil, nil, errors.Capture(err)
	}
	return controllerConfig, modelConfig, nil
}
