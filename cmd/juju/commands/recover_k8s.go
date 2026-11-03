// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package commands

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"time"

	"github.com/juju/errors"
	"github.com/juju/names/v6"

	"github.com/juju/juju/api"
	"github.com/juju/juju/caas"
	"github.com/juju/juju/cmd/cmd"
	"github.com/juju/juju/core/base"
	"github.com/juju/juju/core/model"
	"github.com/juju/juju/domain/recovery"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/environs/cloudspec"
	environscmd "github.com/juju/juju/environs/cmd"
	"github.com/juju/juju/internal/cloudconfig/instancecfg"
	"github.com/juju/juju/internal/cloudconfig/podcfg"
	"github.com/juju/juju/internal/password"
	internalrecovery "github.com/juju/juju/internal/recovery"
)

func (c *recoveryCommand) provisionK8sRecoveryController(ctx *cmd.Context) (resultErr error) {
	info := c.recoveryInfo
	provider, err := environs.Provider(info.CloudType)
	if err != nil {
		return err
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
		return err
	}
	certInfo, err := recoveryCertificate(info, controllerConfig)
	if err != nil {
		return err
	}
	stdCtx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()
	provisionCtx := environscmd.BootstrapContext(stdCtx, ctx)
	if err := provider.ValidateCloud(stdCtx, spec); err != nil {
		return err
	}
	architecture := info.ControllerArchitecture
	charmBase, err := base.ParseBaseFromString(info.ControllerCharmBase)
	if err != nil {
		return err
	}
	broker, err := caas.Open(stdCtx, provider, environs.OpenParams{ControllerUUID: info.ControllerUUID, Cloud: spec, Config: modelConfig}, environs.NoopCredentialInvalidator())
	if err != nil {
		return err
	}
	preparer, ok := broker.(environs.RecoveryControllerPreparer)
	if !ok {
		return errors.NotSupportedf("Kubernetes recovery preparation for %T", broker)
	}
	provisioner, ok := broker.(caas.RecoveryControllerProvisioner)
	if !ok {
		return errors.NotSupportedf("Kubernetes recovery provisioning for %T", broker)
	}
	report, err := preparer.PrepareForRecovery(provisionCtx, environs.RecoverySubstrateParams{ControllerUUID: info.ControllerUUID, ControllerName: info.ControllerName, ControllerModelUUID: info.ControllerModelUUID})
	if err != nil {
		return err
	}
	credentialPatches, err := c.checkRecoveryModels(ctx, stdCtx, report, credentials)
	if err != nil {
		return err
	}
	sourceCloud.HostCloudRegion = report.HostCloudRegion
	adminPassword, err := password.RandomPassword()
	if err != nil {
		return err
	}
	sourceConstraints := info.Constraints
	sourceConstraints.Arch = &architecture
	cons, err := internalrecovery.SizeRecoveryDisk(info, sourceConstraints)
	if err != nil {
		return err
	}
	pcfg, err := podcfg.NewBootstrapControllerPodConfig(controllerConfig, info.ControllerName, "ubuntu", cons)
	if err != nil {
		return err
	}
	applyArchivedK8sProvisioning(pcfg, info, charmBase)
	pcfg.APIInfo = &api.Info{Password: adminPassword, CACert: info.CACert, ModelTag: names.NewModelTag(info.ControllerModelUUID)}
	pcfg.Bootstrap.ControllerAgentInfo = certInfo
	pcfg.Bootstrap.ControllerCloud = sourceCloud
	pcfg.Bootstrap.ControllerCloudRegion = region
	pcfg.Bootstrap.ControllerCloudCredential = credentials.credential
	pcfg.Bootstrap.ControllerCloudCredentialName = credentials.name
	pcfg.Bootstrap.ControllerModelConfig = broker.Config()
	pcfg.Bootstrap.ControllerConfig = controllerConfig
	pcfg.Bootstrap.AgentVersion = info.AgentVersion
	pcfg.Bootstrap.Timeout = 20 * time.Minute
	archivePath := filepath.Join(pcfg.DataDir, "recovery", "archive.tar.gz")
	params, err := (internalrecovery.Params{Credentials: credentialPatches, ArchivePath: archivePath, SHA256: info.Checksum, AgentVersion: info.AgentVersion, MachineName: "0", Node: pcfg.Bootstrap.StateInitializationParams}).Marshal()
	if err != nil {
		return err
	}
	pcfg.Initialisation = &podcfg.ControllerInitialisation{Directory: "recovery", Files: map[string]string{path.Base(internalrecovery.ParamsFile): string(params), path.Base(internalrecovery.ModeFile): "true"}, SetupCommand: recoveryPodSetupCommand(pcfg.DataDir, archivePath, pcfg.Bootstrap.Timeout)}
	var cleanup func(context.Context) error
	registered := false
	defer func() {
		resultErr = c.cleanupFailedRecovery(ctx, cleanup, registered, resultErr)
	}()
	registered, err = c.registerRecoveryController(info, controllerConfig, broker.Config(), spec, credentials.name, adminPassword, model.CAAS)
	if err != nil {
		return err
	}
	cleanup, err = provisioner.ProvisionRecoveryController(provisionCtx, caas.RecoveryControllerParams{PodConfig: pcfg, Archive: instancecfg.InitialisationFile{Source: ctx.AbsPath(c.archivePath), Destination: archivePath, SHA256: info.Checksum}})
	if err != nil {
		return err
	}
	return errors.New("controller recovery initialisation is not available in this build")
}

func applyArchivedK8sProvisioning(pcfg *podcfg.ControllerPodConfig, info *recovery.ArchiveInfo, charmBase base.Base) {
	pcfg.JujuVersion, pcfg.AgentImage, pcfg.CharmBase = info.AgentVersion, info.Kubernetes.AgentImage, charmBase
	pcfg.CharmImage = info.Kubernetes.CharmImage
	pcfg.RecoveryStorageClass = &info.Kubernetes.StorageClass
	pcfg.RecoveryServiceAnnotations = info.Kubernetes.ServiceAnnotations
	pcfg.RecoveryLoadBalancerIP = info.Kubernetes.LoadBalancerIP
	pcfg.Bootstrap.ControllerServiceType = info.Kubernetes.ServiceType
	pcfg.Bootstrap.ControllerExternalName = info.Kubernetes.ExternalName
	pcfg.Bootstrap.ControllerExternalIPs = info.Kubernetes.ExternalIPs
}

func recoveryPodSetupCommand(dataDir, archivePath string, timeout time.Duration) string {
	return fmt.Sprintf(`controller_id="${HOSTNAME##*-}"
if [ "${controller_id}" = "0" ]; then
    if ! test -e %q && ! test -e %q; then
        until test -e %q; do sleep 1; done
    fi
    "$JUJU_TOOLS_DIR/jujuagentd" recovery-state --data-dir %q --timeout %s --show-log || exit 1
else
    until test -e "$JUJU_DATA_DIR/agents/controller-${controller_id}/agent.conf"; do sleep 1; done
fi`, filepath.Join(dataDir, internalrecovery.InitialisedFile), filepath.Join(dataDir, internalrecovery.StartedFile), archivePath, dataDir, timeout.String())
}
