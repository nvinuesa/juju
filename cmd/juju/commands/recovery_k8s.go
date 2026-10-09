// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package commands

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"strings"
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
	"github.com/juju/juju/core/constraints"
	"github.com/juju/juju/core/model"
	"github.com/juju/juju/core/network"
	"github.com/juju/juju/core/semversion"
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
	image, err := recoveryControllerImage(stdCtx, controllerConfig, info.AgentVersion, architecture)
	if err != nil {
		return errors.Annotate(err, "resolving exact recovery agent image")
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
	report, err := preparer.PrepareForRecovery(provisionCtx, environs.RecoverySubstrateParams{ControllerUUID: info.ControllerUUID, ControllerName: info.ControllerName, ControllerModelUUID: info.ControllerModelUUID, Models: recoveryModels(info)})
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
	pcfg, err := podcfg.NewBootstrapControllerPodConfig(controllerConfig, info.ControllerName, "ubuntu", constraints.Value{Arch: &architecture})
	if err != nil {
		return err
	}
	pcfg.JujuVersion, pcfg.AgentImage, pcfg.CharmBase = info.AgentVersion, image, charmBase
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
	params, err := (internalrecovery.Params{ArchivePath: archivePath, SHA256: info.Checksum, AgentVersion: info.AgentVersion, MachineName: "0", Node: pcfg.Bootstrap.StateInitializationParams}).Marshal()
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
	if err := c.updateRecoveryEndpoints(controllerConfig, network.ProviderAddresses(service.Addresses)); err != nil {
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

func recoveryControllerImage(ctx context.Context, cfg controller.Config, version semversion.Number, architecture string) (string, error) {
	image, err := podcfg.GetJujuOCIImagePathFromControllerCfg(cfg, version)
	if err != nil {
		return "", err
	}
	ref, err := reference.ParseNormalizedNamed(image)
	if err != nil {
		return "", err
	}
	tag, ok := ref.(reference.Tagged)
	if !ok || tag.Tag() != version.String() {
		return "", errors.New("recovery image must use the full archived version tag")
	}
	details, err := docker.NewImageRepoDetails(cfg.CAASImageRepo())
	if err != nil {
		return "", err
	}
	if cfg.CAASOperatorImagePath() != "" && details.Repository != "" {
		repository, err := reference.ParseNormalizedNamed(details.Repository)
		if err != nil {
			return "", err
		}
		if details.IsPrivate() && reference.Domain(repository) != reference.Domain(ref) {
			return "", errors.New("archived image credentials belong to a different registry than the controller image")
		}
		if details.ServerAddress != "" {
			address := details.ServerAddress
			if !strings.Contains(address, "://") {
				address = "https://" + address
			}
			endpoint, err := url.Parse(address)
			if err != nil || endpoint.Host != reference.Domain(ref) {
				return "", errors.New("archived image registry endpoint disagrees with the controller image")
			}
		}
	}
	imageName := path.Base(ref.Name())
	details.Repository = strings.TrimSuffix(ref.Name(), "/"+imageName)
	digest, err := registry.ResolveImage(ctx, details, imageName, tag.Tag(), architecture)
	if err != nil {
		return "", err
	}
	return ref.Name() + "@" + digest, nil
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
