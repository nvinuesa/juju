// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package commands

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"time"

	"github.com/distribution/reference"
	"github.com/juju/errors"
	"github.com/juju/names/v6"

	"github.com/juju/juju/api"
	"github.com/juju/juju/caas"
	"github.com/juju/juju/cmd/cmd"
	"github.com/juju/juju/cmd/juju/common"
	"github.com/juju/juju/cmd/modelcmd"
	"github.com/juju/juju/controller"
	"github.com/juju/juju/core/base"
	"github.com/juju/juju/core/model"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/environs/cloudspec"
	environscmd "github.com/juju/juju/environs/cmd"
	"github.com/juju/juju/internal/cloudconfig/instancecfg"
	"github.com/juju/juju/internal/cloudconfig/podcfg"
	"github.com/juju/juju/internal/docker"
	"github.com/juju/juju/internal/docker/registry"
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
	image, err := recoveryControllerImage(stdCtx, controllerConfig, info.Kubernetes.AgentImage, architecture)
	if err != nil {
		return errors.Annotate(err, "resolving exact recovery agent image")
	}
	charmImage, err := recoveryControllerImage(stdCtx, controllerConfig, info.Kubernetes.CharmImage, architecture)
	if err != nil {
		return errors.Annotate(err, "verifying archived controller charm image")
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
	if !report.Empty() {
		for _, name := range report.MissingWorkloads {
			ctx.Warningf("Missing workload objects: %s; reconciliation will recreate them", name)
		}
		for _, name := range report.MissingPersistentVolumeClaims {
			ctx.Warningf("Missing volume: %s; a recreated volume will be empty", name)
		}
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
	pcfg.JujuVersion, pcfg.AgentImage, pcfg.CharmBase = info.AgentVersion, image, charmBase
	pcfg.CharmImage = charmImage
	pcfg.RecoveryStorageClass = &info.Kubernetes.StorageClass
	pcfg.RecoveryServiceAnnotations = info.Kubernetes.ServiceAnnotations
	pcfg.RecoveryLoadBalancerIP = info.Kubernetes.LoadBalancerIP
	pcfg.Bootstrap.ControllerServiceType = info.Kubernetes.ServiceType
	pcfg.Bootstrap.ControllerExternalName = info.Kubernetes.ExternalName
	pcfg.Bootstrap.ControllerExternalIPs = info.Kubernetes.ExternalIPs
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
		if resultErr == nil {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
		defer cancel()
		if cleanup != nil {
			if err := cleanup(cleanupCtx); err != nil {
				resultErr = errors.Annotatef(resultErr, "removing replacement namespace: %v", err)
			}
		}
		if registered {
			if err := c.ClientStore().RemoveController(info.ControllerName); err != nil {
				resultErr = errors.Annotatef(resultErr, "removing replacement registration: %v", err)
			}
		}
	}()
	registered, err = c.registerRecoveryController(info, controllerConfig, broker.Config(), spec, credentials.name, adminPassword, model.CAAS)
	if err != nil {
		return err
	}
	cleanup, err = provisioner.ProvisionRecoveryController(provisionCtx, caas.RecoveryControllerParams{PodConfig: pcfg, Archive: instancecfg.InitialisationFile{Source: ctx.AbsPath(c.archivePath), Destination: archivePath, SHA256: info.Checksum}})
	if err != nil {
		return err
	}
	service, err := broker.GetService(stdCtx, "controller", true)
	if err != nil {
		return err
	}
	if service == nil || len(service.Addresses) == 0 {
		return errors.New("replacement controller service has no API addresses")
	}
	if err := saveControllerConnection(stdCtx, c.ClientStore(), info.ControllerName, broker, controllerConfig, info.AgentVersion.String(), service.Addresses); err != nil {
		return err
	}
	if err := c.SetModelIdentifier(modelcmd.JoinModelName(info.ControllerName, "admin/controller"), false); err != nil {
		return err
	}
	if err := waitForAgentInitialisation(provisionCtx, &c.ModelCommandBase, true, info.ControllerName, common.TryAPI); err != nil {
		return err
	}
	c.printRecoverySummary(ctx, info, report)
	return nil
}

func recoveryControllerImage(ctx context.Context, cfg controller.Config, image, architecture string) (string, error) {
	ref, err := reference.ParseNormalizedNamed(image)
	if err != nil {
		return "", err
	}
	if _, ok := ref.(reference.Canonical); !ok {
		return "", errors.New("archive does not record an immutable source image")
	}
	details, err := docker.NewImageRepoDetails(cfg.CAASImageRepo())
	if err != nil {
		return "", err
	}
	return registry.ResolveReference(ctx, image, details, architecture)
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
