// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package commands

import (
	"bufio"
	"context"
	"fmt"
	"maps"
	"os"
	"sort"
	"strings"

	jujuclock "github.com/juju/clock"
	"github.com/juju/errors"
	"github.com/juju/schema"
	"github.com/juju/utils/v4/keyvalues"
	jujusshtools "github.com/juju/utils/v4/ssh"

	"github.com/juju/juju/caas"
	k8s "github.com/juju/juju/caas/kubernetes"
	jujucloud "github.com/juju/juju/cloud"
	"github.com/juju/juju/cmd/cmd"
	"github.com/juju/juju/cmd/constants"
	"github.com/juju/juju/cmd/juju/common"
	cmdcontroller "github.com/juju/juju/cmd/juju/controller"
	cmdmodel "github.com/juju/juju/cmd/juju/model"
	"github.com/juju/juju/cmd/modelcmd"
	"github.com/juju/juju/controller"
	corebase "github.com/juju/juju/core/base"
	"github.com/juju/juju/core/constraints"
	"github.com/juju/juju/core/instance"
	"github.com/juju/juju/core/network"
	"github.com/juju/juju/core/semversion"
	corestorage "github.com/juju/juju/core/storage"
	jujuversion "github.com/juju/juju/core/version"
	"github.com/juju/juju/domain/deployment/charm"
	"github.com/juju/juju/domain/recovery"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/environs/bootstrap"
	environscloudspec "github.com/juju/juju/environs/cloudspec"
	environscmd "github.com/juju/juju/environs/cmd"
	"github.com/juju/juju/environs/config"
	"github.com/juju/juju/environs/sync"
	"github.com/juju/juju/internal/docker"
	"github.com/juju/juju/internal/featureflag"
	"github.com/juju/juju/internal/naturalsort"
	k8sconstants "github.com/juju/juju/internal/provider/kubernetes/constants"
	"github.com/juju/juju/internal/provider/lxd/lxdnames"
	"github.com/juju/juju/internal/proxy"
	"github.com/juju/juju/internal/ssh"
	"github.com/juju/juju/internal/storage"
	"github.com/juju/juju/internal/uuid"
	"github.com/juju/juju/juju"
	"github.com/juju/juju/juju/osenv"
)

// controllerProvisioner holds the shared provisioning configuration and
// machinery used by bootstrap and recovery.
type controllerProvisioner struct {
	modelcmd.ModelCommandBase

	clock jujuclock.Clock

	Constraints             constraints.Value
	ConstraintsStr          common.ConstraintsFlag
	BootstrapConstraints    constraints.Value
	BootstrapConstraintsStr common.BootstrapConstraintsFlag
	BootstrapBase           string
	BootstrapImage          string
	BuildAgent              bool
	MetadataSource          string
	Placement               string
	KeepBrokenEnvironment   bool
	AutoUpgrade             bool
	AgentVersionParam       string
	AgentVersion            *semversion.Number
	config                  common.ConfigFlag
	modelDefaults           common.ConfigFlag
	storagePool             common.ConfigFlag

	showClouds          bool
	showRegionsForCloud string
	controllerName      string
	CredentialName      string
	Cloud               string
	Region              string
	noSwitch            bool
	interactive         bool

	ControllerCharmPath       string
	ControllerCharmChannelStr string
	ControllerCharmChannel    charm.Channel

	// RecoveryPath and RecoverySHA256 carry `juju recovery`:
	// the backup archive to recover onto the fresh controller and the
	// operator-supplied archive checksum.
	RecoveryPath   string
	RecoverySHA256 string

	// recoveryInfo is the validated archive summary produced by the
	// recovery preflight. It is nil for a normal bootstrap.
	recoveryInfo *recovery.ArchiveInfo

	// recoveryStartedInstanceID is the id of the replacement instance
	// a recovery bootstrap started, stashed once the bootstrap reports
	// success so the failure cleanup can stop it when a later failure
	// carries no instance-id-marked error.
	recoveryStartedInstanceID instance.Id

	// Force is used to allow a bootstrap to be run on unsupported series.
	Force bool
}

func (c *controllerProvisioner) configDetails() ConfigCategoryKeys {
	categoryKeys := ConfigCategoryKeys{}

	if modelCgf, err := cmdmodel.ConfigDetails(); err == nil {
		categoryKeys.ModelKeys = modelCgf
	}
	if controllerCgf, err := cmdcontroller.ConfigDetailsAll(); err == nil {
		categoryKeys.ControllerKeys = controllerCgf
	}

	categoryKeys.BootstrapKeys = make(map[string]common.PrintConfigSchema, len(bootstrap.BootstrapConfigSchema()))
	for key, attr := range bootstrap.BootstrapConfigSchema() {
		categoryKeys.BootstrapKeys[key] = common.PrintConfigSchema{
			Description: attr.Description,
			Type:        string(attr.Type),
		}
	}
	return categoryKeys
}

func (c *controllerProvisioner) setControllerName(controllerName string) {
	c.controllerName = strings.ToLower(controllerName)
}

func parseControllerCharmChannel(channelStr string) (charm.Channel, error) {
	ch, err := charm.ParseChannel(channelStr)
	if err != nil {
		return charm.Channel{}, err
	}

	if ch.Track == "" {
		ch.Track = fmt.Sprintf("%d.%d", jujuversion.Current.Major, jujuversion.Current.Minor)
	}
	if ch.Risk == "" {
		ch.Risk = charm.Stable
	}
	return ch, nil
}

// BootstrapInterface provides bootstrap functionality that Run calls to support cleaner testing.
type BootstrapInterface interface {
	// Bootstrap bootstraps a controller.
	Bootstrap(ctx environs.BootstrapContext, environ environs.BootstrapEnviron,
		args bootstrap.BootstrapParams) error

	// CloudDetector returns a CloudDetector for the given provider,
	// if the provider supports it.
	CloudDetector(environs.EnvironProvider) (environs.CloudDetector, bool)

	// CloudRegionDetector returns a CloudRegionDetector for the given provider,
	// if the provider supports it.
	CloudRegionDetector(environs.EnvironProvider) (environs.CloudRegionDetector, bool)

	// CloudFinalizer returns a CloudFinalizer for the given provider,
	// if the provider supports it.
	CloudFinalizer(environs.EnvironProvider) (environs.CloudFinalizer, bool)
}

type bootstrapFuncs struct{}

func (b bootstrapFuncs) Bootstrap(ctx environs.BootstrapContext, env environs.BootstrapEnviron,
	args bootstrap.BootstrapParams) error {
	return bootstrap.Bootstrap(ctx, env, args)
}

func (b bootstrapFuncs) CloudDetector(provider environs.EnvironProvider) (environs.CloudDetector, bool) {
	detector, ok := provider.(environs.CloudDetector)
	return detector, ok
}

func (b bootstrapFuncs) CloudRegionDetector(provider environs.EnvironProvider) (environs.CloudRegionDetector, bool) {
	detector, ok := provider.(environs.CloudRegionDetector)
	return detector, ok
}

func (b bootstrapFuncs) CloudFinalizer(provider environs.EnvironProvider) (environs.CloudFinalizer, bool) {
	finalizer, ok := provider.(environs.CloudFinalizer)
	return finalizer, ok
}

var getBootstrapFuncs = func() BootstrapInterface {
	return &bootstrapFuncs{}
}

var (
	bootstrapPrepareController = bootstrap.PrepareController
	environsDestroy            = environs.Destroy
	waitForAgentInitialisation = common.WaitForAgentInitialisation
)

var ambiguousDetectedCredentialError = errors.New(`
more than one credential detected
run juju autoload-credentials and specify a credential using the --credential argument`[1:],
)

var ambiguousCredentialError = errors.New(`
more than one credential is available
specify a credential using the --credential argument`[1:],
)

func (c *controllerProvisioner) parseConstraints(ctx *cmd.Context) (err error) {
	allAliases := map[string]string{}
	defer common.WarnConstraintAliases(ctx, allAliases)
	if c.ConstraintsStr.String() != "" {
		cons, aliases, err := constraints.ParseWithAliases(strings.Join(c.ConstraintsStr, " "))
		maps.Copy(allAliases, aliases)
		if err != nil {
			return err
		}
		c.Constraints = cons
	}
	if c.BootstrapConstraintsStr.String() != "" {
		cons, aliases, err := constraints.ParseWithAliases(strings.Join(c.BootstrapConstraintsStr, " "))
		maps.Copy(allAliases, aliases)
		if err != nil {
			return err
		}
		c.BootstrapConstraints = cons
	}
	return nil
}

func (c *controllerProvisioner) provisionController(ctx *cmd.Context) (resultErr error) {
	var isCAASController bool
	if err := c.parseConstraints(ctx); err != nil {
		return err
	}

	// Start by checking for usage errors, requests for information
	finished, err := c.handleCommandLineErrorsAndInfoRequests(ctx)
	if err != nil {
		return errors.Trace(err)
	}
	if finished {
		return nil
	}

	// Run interactive bootstrap if needed/asked for
	if c.interactive {
		if err := c.runInteractive(ctx); err != nil {
			return errors.Trace(err)
		}
		// now run normal bootstrap using info gained above.
	}

	cloud, provider, err := c.cloud(ctx)
	if err != nil {
		return errors.Trace(err)
	}

	// If region is specified by the user, validate it here.
	// lp#1632735
	if c.Region != "" {
		_, err := jujucloud.RegionByName(cloud.Regions, c.Region)
		if err != nil {
			allRegions := make([]string, len(cloud.Regions))
			for i, one := range cloud.Regions {
				allRegions[i] = one.Name
			}
			if len(allRegions) > 0 {
				naturalsort.Sort(allRegions)
				plural := "s are"
				if len(allRegions) == 1 {
					plural = " is"
				}
				ctx.Infof("Available cloud region%v %v", plural, strings.Join(allRegions, ", "))
			}
			return errors.NotValidf("region %q for cloud %q", c.Region, c.Cloud)
		}
	}

	// Custom clouds may not have explicitly declared support for any auth-
	// types, in which case we'll assume that they support everything that
	// the provider supports.
	if len(cloud.AuthTypes) == 0 {
		for authType := range provider.CredentialSchemas() {
			cloud.AuthTypes = append(cloud.AuthTypes, authType)
		}
	}

	credentials, regionName, err := c.credentialsAndRegionName(ctx, provider, cloud)
	if err != nil {
		if errors.Is(err, errors.NotFound) {
			err = errors.NewNotFound(nil,
				fmt.Sprintf("%v\nSee `juju add-credential %s --help` for instructions", err, cloud.Name))
		}

		if err == cmd.ErrSilent {
			return err
		}
		return errors.Trace(err)
	}

	if c.recoveryInfo != nil {
		regionName = c.recoveryInfo.Region
	}
	region, err := common.ChooseCloudRegion(cloud, regionName)
	if err != nil {
		return errors.Trace(err)
	}

	if c.controllerName == "" {
		c.setControllerName(defaultControllerName(cloud.Name, region.Name))
	}

	// set a Region so it's config can be found below.
	if c.Region == "" {
		c.Region = region.Name
	}

	bootstrapCfg, err := c.bootstrapConfigs(ctx, cloud, provider)
	if err != nil {
		return errors.Trace(err)
	}

	isCAASController = jujucloud.CloudIsCAAS(cloud)
	if !isCAASController {
		if bootstrapCfg.bootstrap.ControllerServiceType != "" ||
			bootstrapCfg.bootstrap.ControllerExternalName != "" ||
			len(bootstrapCfg.bootstrap.ControllerExternalIPs) > 0 {
			return errors.Errorf("%q, %q and %q\nare only allowed for kubernetes controllers",
				bootstrap.ControllerServiceType, bootstrap.ControllerExternalName, bootstrap.ControllerExternalIPs)
		}
	}

	if bootstrapCfg.controller.ControllerName() != "" {
		return errors.NewNotValid(nil, "controller name cannot be set via config, please use cmd args")
	}

	// Read existing current controller so we can clean up on error.
	var oldCurrentController string
	store := c.ClientStore()
	oldCurrentController, err = modelcmd.DetermineCurrentController(store)
	if errors.Is(err, errors.NotFound) {
		oldCurrentController = ""
	} else if err != nil {
		return errors.Annotate(err, "error reading current controller")
	}

	defer func() {
		if resultErr == nil || errors.Is(resultErr, errors.AlreadyExists) {
			return
		}
		if oldCurrentController != "" {
			if err := store.SetCurrentController(oldCurrentController); err != nil {
				logger.Errorf(context.TODO(),
					"cannot reset current controller to %q: %v",
					oldCurrentController, err,
				)
			}
		}
		if err := store.RemoveController(c.controllerName); err != nil {
			logger.Errorf(context.TODO(),
				"cannot destroy newly created controller %q details: %v",
				c.controllerName, err,
			)
		}
	}()

	var bootstrapBase corebase.Base
	if c.BootstrapBase != "" {
		var err error
		bootstrapBase, err = corebase.ParseBaseFromString(c.BootstrapBase)
		if err != nil {
			return errors.NotValidf("bootstrap base %q", c.BootstrapBase)
		}
	}

	bootstrapCfg.controller[controller.ControllerName] = c.controllerName

	// Handle Ctrl-C during bootstrap by asking the bootstrap process to stop
	// early (and the above will then clean up resources).
	interrupted := make(chan os.Signal, 1)
	defer close(interrupted)

	ctx.InterruptNotify(interrupted)
	defer ctx.StopInterruptNotify(interrupted)

	var cancel context.CancelFunc
	stdCtx, cancel := context.WithTimeout(ctx, bootstrapCfg.bootstrap.BootstrapTimeout)

	defer func() {
		// If the context is an error state, then don't continue on processing
		// the bootstrap command.
		if stdCtx.Err() != nil {
			return
		}

		resultErr = handleChooseCloudRegionError(ctx, resultErr)
		if !c.showClouds && resultErr == nil {
			workloadType := ""
			if isCAASController {
				workloadType = "k8s "
			}

			ctx.Infof(`
Now you can run
	juju add-model <model-name>
to create a new model to deploy %sworkloads.
`, workloadType)
		}
	}()

	go func() {
		for range interrupted {
			select {
			case <-stdCtx.Done():
				// Ctrl-C already pressed
				return

			default:
				// Newline prefix is intentional, so output appears as
				// "^C\nCtrl-C pressed" instead of "^CCtrl-C pressed".
				_, _ = fmt.Fprintln(ctx.GetStderr(), "\nCtrl-C pressed, attempting to stop bootstrap and clean up resources")
				cancel()
			}
		}
	}()

	bootstrapCtx := environscmd.BootstrapContext(stdCtx, ctx)
	bootstrapPrepareParams := bootstrap.PrepareParams{
		ModelConfig:      bootstrapCfg.bootstrapModel,
		ControllerConfig: bootstrapCfg.controller,
		ControllerName:   c.controllerName,
		Cloud: environscloudspec.CloudSpec{
			Type:             cloud.Type,
			Name:             cloud.Name,
			Region:           region.Name,
			Endpoint:         region.Endpoint,
			IdentityEndpoint: region.IdentityEndpoint,
			StorageEndpoint:  region.StorageEndpoint,
			Credential:       credentials.credential,
			CACertificates:   cloud.CACertificates,
			SkipTLSVerify:    cloud.SkipTLSVerify,
		},
		CredentialName: credentials.name,
		AdminSecret:    bootstrapCfg.bootstrap.AdminSecret,
	}
	environ, err := bootstrapPrepareController(
		isCAASController, bootstrapCtx, store, bootstrapPrepareParams,
	)
	if err != nil {
		return errors.Trace(err)
	}

	// Validate the storage provider config.
	registry := environ
	for poolName, cfg := range bootstrapCfg.storagePools {
		poolAttrs := make(storage.Attrs)
		maps.Copy(poolAttrs, cfg)
		poolType, _ := poolAttrs[corestorage.BootstrapStoragePoolTypeKey].(string)
		delete(poolAttrs, corestorage.BootstrapStoragePoolNameKey)
		delete(poolAttrs, corestorage.BootstrapStoragePoolTypeKey)
		sc, err := storage.NewConfig(poolName, storage.ProviderType(poolType), poolAttrs)
		if err != nil {
			return errors.Trace(err)
		}
		p, err := registry.StorageProvider(sc.Provider())
		if err != nil {
			return errors.Annotatef(err, "invalid storage provider config")
		}
		err = p.ValidateConfig(sc)
		if err != nil {
			return errors.NewNotValid(err, "invalid storage provider config")
		}
	}

	supportedBootstrapBases := corebase.ControllerBases()
	logger.Tracef(context.TODO(), "supported bootstrap bases %v", supportedBootstrapBases)
	bootstrapSSHKeyFiles := jujusshtools.PrivateKeyFiles()
	if len(bootstrapSSHKeyFiles) == 0 {
		return errors.New("no Juju SSH keys available for bootstrap")
	}
	sort.Strings(bootstrapSSHKeyFiles)
	// Keep both slices in the same deterministic order so each private key is
	// paired with the corresponding public key during bootstrap.
	bootstrapSSHAuthorizedKeys, err := ssh.PublicKeysForPrivateKeyFiles(bootstrapSSHKeyFiles)
	if err != nil {
		return errors.Annotate(err, "reading Juju bootstrap SSH key")
	}

	bootstrapParams := bootstrap.BootstrapParams{
		ControllerName:                c.controllerName,
		BootstrapBase:                 bootstrapBase,
		SupportedBootstrapBases:       supportedBootstrapBases,
		BootstrapImage:                c.BootstrapImage,
		Placement:                     c.Placement,
		BuildAgent:                    c.BuildAgent,
		BuildAgentTarball:             sync.BuildAgentTarball,
		AgentVersion:                  c.AgentVersion,
		Cloud:                         cloud,
		CloudRegion:                   region.Name,
		ControllerConfig:              bootstrapCfg.controller,
		ControllerInheritedConfig:     bootstrapCfg.inheritedControllerAttrs,
		ControllerModelAuthorizedKeys: bootstrapCfg.bootstrap.AuthorizedKeys,
		BootstrapSSHAuthorizedKeys:    bootstrapSSHAuthorizedKeys,
		RegionInheritedConfig:         cloud.RegionConfig,
		AdminSecret:                   bootstrapCfg.bootstrap.AdminSecret,
		CAPrivateKey:                  bootstrapCfg.bootstrap.CAPrivateKey,
		SSHServerHostKey:              bootstrapCfg.bootstrap.SSHServerHostKey,
		ControllerServiceType:         bootstrapCfg.bootstrap.ControllerServiceType,
		ControllerExternalName:        bootstrapCfg.bootstrap.ControllerExternalName,
		ControllerExternalIPs:         append([]string(nil), bootstrapCfg.bootstrap.ControllerExternalIPs...),
		StoragePools:                  bootstrapCfg.storagePools,
		ControllerCharmPath:           c.ControllerCharmPath,
		ControllerCharmChannel:        c.ControllerCharmChannel,
		DialOpts: environs.BootstrapDialOpts{
			IdentityFiles:  bootstrapSSHKeyFiles,
			Timeout:        bootstrapCfg.bootstrap.BootstrapTimeout,
			RetryDelay:     bootstrapCfg.bootstrap.BootstrapRetryDelay,
			AddressesDelay: bootstrapCfg.bootstrap.BootstrapAddressesDelay,
		},
		Force: c.Force,
	}
	if c.recoveryInfo != nil {
		bootstrapParams.Recovery = &bootstrap.RecoveryParams{
			SourcePath:          c.RecoveryPath,
			SHA256:              c.RecoverySHA256,
			ControllerUUID:      c.recoveryInfo.ControllerUUID,
			ControllerName:      c.recoveryInfo.ControllerName,
			ControllerModelUUID: c.recoveryInfo.ControllerModelUUID,
			Models:              recoveryModels(c.recoveryInfo),
		}
	}

	if err := store.SetCurrentModel(c.controllerName, ""); err != nil {
		return errors.Trace(err)
	}

	if !c.noSwitch {
		// set the current controller.
		if err := store.SetCurrentController(c.controllerName); err != nil {
			return errors.Trace(err)
		}
	}

	cloudRegion := c.Cloud
	if region.Name != "" {
		cloudRegion = fmt.Sprintf("%s/%s", cloudRegion, region.Name)
	}
	ctx.Infof(
		"Creating Juju controller %q on %s",
		c.controllerName, cloudRegion,
	)

	// handleBootstrapErrorFunc is a function that will be called to clean up
	// the environment if the bootstrap process fails.
	handleBootstrapErrorFunc := func(bootstrapErr error) error {
		if c.RecoveryPath != "" {
			return cleanupFailedRecovery(
				c.controllerName, environ, bootstrapErr, c.recoveryStartedInstanceID,
				ctx, store,
			)
		}
		return environsDestroy(
			c.controllerName, environ, ctx, store,
		)
	}

	// If we error out for any reason, clean up the environment.
	defer func() {
		if resultErr == nil {
			return
		}

		if c.KeepBrokenEnvironment {
			ctx.Infof(`
bootstrap failed but --keep-broken was specified.
This means that cloud resources are left behind, but not registered to
your local client, as the controller was not successfully created.
However, you should be able to ssh into the machine using the user "ubuntu" and
their IP address for diagnosis and investigation.
When you are ready to clean up the failed controller, use your cloud console or
equivalent CLI tools to terminate the instances and remove remaining resources.

See %s.`[1:], "`juju kill-controller`")
			return
		}

		logger.Errorf(context.TODO(), "%v", resultErr)
		logger.Debugf(context.TODO(), "(error details: %v)", errors.Details(resultErr))
		// The cleanup needs the ORIGINAL error: resultErr is set to
		// cmd.ErrSilent below to prevent logging it twice, which would
		// erase the bootstrap-instance marker the recovery cleanup reads.
		bootstrapErr := resultErr
		// Set resultErr to cmd.ErrSilent to prevent
		// logging the error twice.
		resultErr = cmd.ErrSilent
		handleBootstrapError(ctx, func() error {
			return handleBootstrapErrorFunc(bootstrapErr)
		})
	}()

	if envMetadataSrc := os.Getenv(constants.EnvJujuMetadataSource); c.MetadataSource == "" && envMetadataSrc != "" {
		c.MetadataSource = envMetadataSrc
		ctx.Infof("Using metadata source directory %q", c.MetadataSource)
	}

	// If --metadata-source is specified, override the default tools metadata source so
	// SyncTools can use it, and also upload any image metadata.
	if c.MetadataSource != "" {
		bootstrapParams.MetadataDir = ctx.AbsPath(c.MetadataSource)
	}

	constraintsValidator, err := environ.ConstraintsValidator(ctx)
	if err != nil {
		return errors.Trace(err)
	}

	// Merge in any space constraints that should be implied from controller
	// space config.
	// Do it before calling merge, because the constraints will be validated
	// there.
	bootstrapConstraints := c.Constraints
	bootstrapConstraints.Spaces = bootstrapCfg.controller.AsSpaceConstraints(bootstrapConstraints.Spaces)

	// Merge environ and bootstrap-specific constraints.
	bootstrapParams.BootstrapConstraints, err = constraintsValidator.Merge(bootstrapConstraints, c.BootstrapConstraints)
	if err != nil {
		return errors.Trace(err)
	}
	logger.Infof(context.TODO(), "combined bootstrap constraints: %v", bootstrapParams.BootstrapConstraints)
	unsupported, err := constraintsValidator.Validate(bootstrapParams.BootstrapConstraints)
	if err != nil {
		return errors.Trace(err)
	}
	if len(unsupported) > 0 {
		logger.Warningf(context.TODO(),
			"unsupported constraints: %v", strings.Join(unsupported, ","))
	}

	bootstrapParams.ModelConstraints = c.Constraints

	if credentials.name == "" {
		// credentialName will be empty if the credential was detected.
		// We must supply a name for the credential in the database,
		// so choose one.
		credentials.name = credentials.detectedName
	}
	bootstrapParams.CloudCredential = credentials.credential
	bootstrapParams.CloudCredentialName = credentials.name

	// See if there's any additional agent environment options required.
	// eg JUJU_AGENT_TESTING_OPTIONS=foo=bar,timeout=2s
	// These are written to the agent.conf VALUES section.
	testingOptionsStr := os.Getenv("JUJU_AGENT_TESTING_OPTIONS")
	if len(testingOptionsStr) > 0 {
		opts, err := keyvalues.Parse(
			strings.Split(
				strings.ReplaceAll(testingOptionsStr, " ", ""), ","), false)
		if err != nil {
			return errors.Annotatef(err, "invalid JUJU_AGENT_TESTING_OPTIONS env value %q", testingOptionsStr)
		}
		for k, v := range opts {
			if bootstrapParams.ExtraAgentValuesForTesting == nil {
				bootstrapParams.ExtraAgentValuesForTesting = map[string]string{}
			}
			bootstrapParams.ExtraAgentValuesForTesting[k] = v
		}
	}

	if cloud.Type == jujucloud.CloudTypeKubernetes {
		if cloud.HostCloudRegion == k8s.K8sCloudOther {
			ctx.Infof("Bootstrap to generic Kubernetes cluster")
		} else {
			ctx.Infof("Bootstrap to Kubernetes cluster identified as %s",
				cloud.HostCloudRegion)
		}

	}

	bootstrapFuncs := getBootstrapFuncs()
	if err = bootstrapFuncs.Bootstrap(
		bootstrapCtx,
		environ,
		bootstrapParams,
	); err != nil {
		return errors.Annotate(err, "failed to bootstrap model")
	}

	if c.recoveryInfo != nil && !isCAASController && bootstrapParams.Recovery != nil {
		// The provider recorded the running replacement's instance id
		// in the shared recovery params; stash it so the deferred
		// cleanup can stop the replacement if a later step fails.
		c.recoveryStartedInstanceID = instance.Id(bootstrapParams.Recovery.StartedInstanceID)
	}

	if err = c.controllerDataRefresher(environ, ctx, bootstrapCfg); err != nil {
		return errors.Trace(err)
	}

	if err = c.SetModelIdentifier(modelcmd.JoinModelName(c.controllerName, bootstrap.ControllerModelName), false); err != nil {
		return errors.Trace(err)
	}

	// To avoid race conditions when running scripted bootstraps, wait
	// for the controller's machine agent to be ready to accept commands
	// before exiting this bootstrap command.
	if err := waitForAgentInitialisation(
		bootstrapCtx,
		&c.ModelCommandBase,
		isCAASController,
		c.controllerName,
		common.TryAPI,
	); err != nil {
		return err
	}

	if c.recoveryInfo != nil {
		c.printRecoverySummary(ctx, c.recoveryInfo, bootstrapParams.Recovery.SubstrateReport)
	}
	return nil
}

func (c *controllerProvisioner) controllerDataRefresher(
	environ environs.BootstrapEnviron,
	ctx context.Context,
	bootstrapCfg bootstrapConfigs,
) error {
	agentVersion := jujuversion.Current
	if c.AgentVersion != nil {
		agentVersion = *c.AgentVersion
	}

	// This logic allows polling for address info later during retries,
	// for example, when a load balancer needs time to be provisioned.
	var addrs []network.ProviderAddress
	var err error
	if env, ok := environ.(environs.InstanceBroker); ok {
		// IAAS.
		addrs, err = common.BootstrapEndpointAddresses(env, ctx)
		if err != nil {
			return errors.Trace(err)
		}
	} else if env, ok := environ.(caas.ServiceManager); ok {
		// CAAS.
		var svc *caas.Service
		svc, err = env.GetService(ctx, k8sconstants.JujuControllerStackName, false)
		if err != nil {
			return errors.Trace(err)
		}
		if len(svc.Addresses) == 0 {
			return errors.NotProvisionedf("k8s controller service %q address", svc.Id)
		}
		addrs = svc.Addresses
	} else {
		// This should never happen.
		return errors.New(
			"supplied BootstrapEnviron implements neither environs.InstanceBroker nor caas.ServiceGetterSetter")
	}

	var proxier proxy.Proxier
	if conInfo, ok := environ.(environs.ConnectorInfo); ok {
		proxier, err = conInfo.ConnectionProxyInfo(ctx)
		if err != nil && !errors.Is(err, errors.NotFound) {
			return errors.Trace(err)
		}
	}

	// Use the retrieved bootstrap machine/service addresses to create
	// host/port endpoints for local storage.
	hps := make([]network.MachineHostPort, len(addrs))
	for i, addr := range addrs {
		hps[i] = network.MachineHostPort{
			MachineAddress: addr.MachineAddress,
			NetPort:        network.NetPort(bootstrapCfg.controller.APIPort()),
		}
	}
	return errors.Annotate(
		juju.UpdateControllerDetailsFromLogin(
			c.ClientStore(),
			c.controllerName,
			juju.UpdateControllerParams{
				AgentVersion:           agentVersion.String(),
				CurrentHostPorts:       []network.MachineHostPorts{hps},
				PublicDNSName:          newStringIfNonEmpty(bootstrapCfg.controller.AutocertDNSName()),
				MachineCount:           new(1),
				Proxier:                proxier,
				ControllerMachineCount: new(1),
			},
		),
		"saving bootstrap endpoint address",
	)
}

func (c *controllerProvisioner) handleCommandLineErrorsAndInfoRequests(ctx *cmd.Context) (bool, error) {
	if c.BootstrapImage != "" {
		if c.BootstrapBase == "" {
			return true, errors.Errorf("--bootstrap-image must be used with --bootstrap-base")
		}
		cons, err := constraints.Merge(c.Constraints, c.BootstrapConstraints)
		if err != nil {
			return true, errors.Trace(err)
		}
		if !cons.HasArch() {
			return true, errors.Errorf("--bootstrap-image must be used with --bootstrap-constraints, specifying architecture")
		}
	}
	if c.showClouds {
		return true, printClouds(ctx, c.ClientStore())
	}
	if c.showRegionsForCloud != "" {
		return true, printCloudRegions(ctx, c.showRegionsForCloud)
	}

	return false, nil
}

func (c *controllerProvisioner) cloud(ctx *cmd.Context) (jujucloud.Cloud, environs.EnvironProvider, error) {
	if c.recoveryInfo != nil {
		cloud := recoveryCloud(c.recoveryInfo)
		provider, err := environs.Provider(cloud.Type)
		if err != nil {
			return jujucloud.Cloud{}, nil, err
		}
		return cloud, provider, nil
	}
	bootstrapFuncs := getBootstrapFuncs()
	fail := func(err error) (jujucloud.Cloud, environs.EnvironProvider, error) {
		return jujucloud.Cloud{}, nil, err
	}

	// Get the cloud definition identified by c.Cloud. If c.Cloud does not
	// identify a cloud in clouds.yaml, then we check if any of the
	// providers can detect a cloud with the given name. Otherwise, if the
	// cloud name identifies a provider *type* (e.g. "openstack"), then we
	// check if that provider can detect cloud regions, and synthesise a
	// cloud with those regions.
	var provider environs.EnvironProvider
	var cloud jujucloud.Cloud
	cloudptr, err := jujucloud.CloudByName(c.Cloud)
	if errors.Is(err, errors.NotFound) {
		cloud, provider, err = c.detectCloud(ctx, bootstrapFuncs)
		if err != nil {
			return fail(errors.Trace(err))
		}
	} else if err != nil {
		return fail(errors.Trace(err))
	} else {
		cloud = *cloudptr
		if err := checkProviderType(cloud.Type); err != nil {
			return fail(errors.Trace(err))
		}
		provider, err = environs.Provider(cloud.Type)
		if err != nil {
			return fail(errors.Trace(err))
		}
	}

	if finalizer, ok := bootstrapFuncs.CloudFinalizer(provider); ok {
		cloud, err = finalizer.FinalizeCloud(ctx, cloud)
		if err != nil {
			return fail(errors.Trace(err))
		}
	}

	if err = c.validateRegion(ctx, &cloud); err != nil {
		return fail(errors.Trace(err))
	}

	// Custom clouds may not have explicitly declared support for any auth-
	// types, in which case we'll assume that they support everything that
	// the provider supports.
	if len(cloud.AuthTypes) == 0 {
		for authType := range provider.CredentialSchemas() {
			cloud.AuthTypes = append(cloud.AuthTypes, authType)
		}
	}

	return cloud, provider, nil
}

func (c *controllerProvisioner) detectCloud(
	ctx *cmd.Context,
	bootstrapFuncs BootstrapInterface,
) (jujucloud.Cloud, environs.EnvironProvider, error) {
	fail := func(err error) (jujucloud.Cloud, environs.EnvironProvider, error) {
		return jujucloud.Cloud{}, nil, err
	}

	// Check if any of the registered providers can give us a cloud with
	// the specified name. The first one wins.
	for _, providerType := range environs.RegisteredProviders() {
		provider, err := environs.Provider(providerType)
		if err != nil {
			return fail(errors.Trace(err))
		}
		cloudDetector, ok := bootstrapFuncs.CloudDetector(provider)
		if !ok {
			continue
		}
		cloud, err := cloudDetector.DetectCloud(c.Cloud)
		if errors.Is(err, errors.NotFound) {
			continue
		} else if err != nil {
			return fail(errors.Trace(err))
		}
		return cloud, provider, nil
	}

	ctx.Verbosef("cloud %q not found, trying as a provider name", c.Cloud)
	provider, err := environs.Provider(c.Cloud)
	if errors.Is(err, errors.NotFound) {
		return fail(errors.NewNotFound(nil,
			fmt.Sprintf("unknown cloud %q, please try %q", c.Cloud, "juju update-public-clouds")))
	} else if err != nil {
		return fail(errors.Trace(err))
	}
	regionDetector, ok := bootstrapFuncs.CloudRegionDetector(provider)
	if !ok {
		ctx.Verbosef(
			"provider %q does not support detecting regions",
			c.Cloud,
		)
		return fail(errors.NewNotFound(nil,
			fmt.Sprintf("unknown cloud %q, please try %q", c.Cloud, "juju update-public-clouds")))
	}

	var cloudEndpoint string
	regions, err := regionDetector.DetectRegions()
	if errors.Is(err, errors.NotFound) {
		// It's not an error to have no regions. If the
		// provider does not support regions, then we
		// reinterpret the supplied region name as the
		// cloud's endpoint. This enables the user to
		// supply, for example, maas/<IP> or unmanaged/<IP>.
		if c.Region != "" {
			ctx.Verbosef("interpreting %q as the cloud endpoint", c.Region)
			cloudEndpoint = c.Region
			c.Region = ""
		}
	} else if err != nil {
		return fail(errors.Annotatef(err,
			"detecting regions for %q cloud provider",
			c.Cloud,
		))
	}
	schemas := provider.CredentialSchemas()
	authTypes := make([]jujucloud.AuthType, 0, len(schemas))
	for authType := range schemas {
		authTypes = append(authTypes, authType)
	}

	// Since we are iterating over a map, lets sort the authTypes so
	// they are always in a consistent order.
	sort.Sort(jujucloud.AuthTypes(authTypes))
	return jujucloud.Cloud{
		Name:      c.Cloud,
		Type:      c.Cloud,
		AuthTypes: authTypes,
		Endpoint:  cloudEndpoint,
		Regions:   regions,
	}, provider, nil
}

func (c *controllerProvisioner) validateRegion(ctx *cmd.Context, cloud *jujucloud.Cloud) error {
	if c.Region == "" {
		return nil
	}
	if _, err := jujucloud.RegionByName(cloud.Regions, c.Region); err == nil {
		return nil
	}
	allRegions := make([]string, len(cloud.Regions))
	for i, one := range cloud.Regions {
		allRegions[i] = one.Name
	}
	if len(allRegions) > 0 {
		naturalsort.Sort(allRegions)
		plural := "s are"
		if len(allRegions) == 1 {
			plural = " is"
		}
		ctx.Infof("Available cloud region%v %v", plural, strings.Join(allRegions, ", "))
	}
	return errors.NotValidf("region %q for cloud %q", c.Region, c.Cloud)
}

type bootstrapCredentials struct {
	credential   *jujucloud.Credential
	name         string
	detectedName string
}

// Get the credentials and region name.
func (c *controllerProvisioner) credentialsAndRegionName(
	ctx *cmd.Context,
	provider environs.EnvironProvider,
	cloud jujucloud.Cloud,
) (
	creds bootstrapCredentials,
	regionName string,
	err error,
) {
	if c.recoveryInfo != nil {
		return c.recoveryCredentials(ctx, provider, cloud)
	}
	store := c.ClientStore()

	// When looking for credentials, we should attempt to see if there are any
	// credentials that should be registered, before we get or detect them
	err = common.RegisterCredentials(ctx, store, provider, modelcmd.RegisterCredentialsParams{
		Cloud: cloud,
	})
	if err != nil {
		logger.Errorf(context.TODO(), "registering credentials errored %s", err)
	}

	var detected bool
	creds.credential, creds.name, regionName, detected, err = common.GetOrDetectCredential(
		ctx, store, provider, modelcmd.GetCredentialsParams{
			Cloud:          cloud,
			CloudRegion:    c.Region,
			CredentialName: c.CredentialName,
		},
	)
	switch errors.Cause(err) {
	case nil:
	case modelcmd.ErrMultipleCredentials:
		return bootstrapCredentials{}, "", ambiguousCredentialError
	case common.ErrMultipleDetectedCredentials:
		return bootstrapCredentials{}, "", ambiguousDetectedCredentialError
	default:
		return bootstrapCredentials{}, "", errors.Trace(err)
	}
	logger.Debugf(context.TODO(),
		"authenticating with region %q and credential %q (%v)",
		regionName, creds.name, creds.credential.Label,
	)
	if detected {
		creds.detectedName = creds.name
		creds.name = ""
	}
	logger.Tracef(context.TODO(), "credential: %v", creds.credential)
	return creds, regionName, nil
}

// bootstrapConfigs is a deconstructed representation of all of the config
// options supplied by a user at bootstrap time into their various buckets.
type bootstrapConfigs struct {
	bootstrapModel           map[string]any
	controller               controller.Config
	bootstrap                bootstrap.Config
	inheritedControllerAttrs map[string]any
	userConfigAttrs          map[string]any
	storagePools             map[string]storage.Attrs
}

func (c *controllerProvisioner) bootstrapConfigs(
	ctx *cmd.Context,
	cloud jujucloud.Cloud,
	provider environs.EnvironProvider,
) (
	bootstrapConfigs,
	error,
) {

	controllerModelUUID, err := uuid.NewUUID()
	if err != nil {
		return bootstrapConfigs{}, errors.Trace(err)
	}
	controllerUUID, err := uuid.NewUUID()
	if err != nil {
		return bootstrapConfigs{}, errors.Trace(err)
	}

	// In recovery mode the replacement takes the source's identities: the
	// controller and controller model UUIDs and the controller CA come
	// from the validated archive, so the whole replacement stack is born
	// with the source identity and surviving agents keep source trust.
	if c.recoveryInfo != nil {
		if controllerModelUUID, err = uuid.UUIDFromString(c.recoveryInfo.ControllerModelUUID); err != nil {
			return bootstrapConfigs{}, errors.Annotate(err, "parsing source controller model UUID")
		}
		if controllerUUID, err = uuid.UUIDFromString(c.recoveryInfo.ControllerUUID); err != nil {
			return bootstrapConfigs{}, errors.Annotate(err, "parsing source controller UUID")
		}
	}

	// Create a model config, and split out any controller
	// and bootstrap config attributes.
	combinedConfig := map[string]any{
		"type":         cloud.Type,
		"name":         bootstrap.ControllerModelName,
		config.UUIDKey: controllerModelUUID.String(),
	}
	if c.recoveryInfo != nil {
		combinedConfig[bootstrap.CACertKey] = c.recoveryInfo.CACert
		combinedConfig[bootstrap.CAPrivateKeyKey] = c.recoveryInfo.CAPrivateKey
	}

	userConfigAttrs, err := c.config.ReadAttrs(ctx)
	if err != nil {
		return bootstrapConfigs{}, errors.Trace(err)
	}

	if c.recoveryInfo != nil {
		userConfigAttrs = make(map[string]any)
		if source, ok := provider.(config.ConfigSchemaSource); ok {
			for key := range source.ConfigSchema() {
				if value, ok := c.recoveryInfo.ModelConfig[key]; ok {
					userConfigAttrs[key] = value
				}
			}
		}
	}
	modelDefaultConfigAttrs, err := c.modelDefaults.ReadAttrs(ctx)
	if err != nil {
		return bootstrapConfigs{}, errors.Trace(err)
	}

	// The provider may define some custom attributes specific
	// to the provider. These will be added to the model config.
	var providerAttrs map[string]any
	if ps, ok := provider.(config.ConfigSchemaSource); ok {
		providerAttrs = make(map[string]any)
		for attr := range ps.ConfigSchema() {
			// Start with the model defaults, and if also specified
			// in the user config attrs, they override the model default.
			if v, ok := modelDefaultConfigAttrs[attr]; ok {
				providerAttrs[attr] = v
			}
			if v, ok := userConfigAttrs[attr]; ok {
				providerAttrs[attr] = v
			}
		}
		fields := schema.FieldMap(ps.ConfigSchema(), ps.ConfigDefaults())
		coercedAttrs, err := fields.Coerce(providerAttrs, nil)
		if err != nil {
			return bootstrapConfigs{},
				errors.Annotatef(err, "invalid attribute value(s) for %v cloud", cloud.Type)
		}
		providerAttrs = coercedAttrs.(map[string]any)
	}

	storagePoolAttrs, err := c.storagePool.ReadAttrs(ctx)
	if err != nil {
		return bootstrapConfigs{}, errors.Trace(err)
	}
	var storagePools map[string]storage.Attrs
	if len(storagePoolAttrs) > 0 {
		poolName, _ := storagePoolAttrs[corestorage.BootstrapStoragePoolNameKey].(string)
		if poolName == "" {
			return bootstrapConfigs{}, errors.NotValidf(
				"storage pool requires a %q key to be set",
				corestorage.BootstrapStoragePoolNameKey,
			)
		}
		poolType, _ := storagePoolAttrs[corestorage.BootstrapStoragePoolTypeKey].(string)
		if poolType == "" {
			return bootstrapConfigs{}, errors.NotValidf(
				"storage pool requires a %q key to be set",
				corestorage.BootstrapStoragePoolTypeKey,
			)
		}
		storagePools = make(map[string]storage.Attrs)
		storagePools[poolName] = storagePoolAttrs
	}

	bootstrapConfigAttrs := make(map[string]any)
	controllerConfigAttrs := make(map[string]any)
	// Based on the attribute names in clouds.yaml, create
	// a map of shared config for all models on this cloud.
	inheritedControllerAttrs := make(map[string]any)
	for k, v := range cloud.Config {
		switch {
		case bootstrap.IsBootstrapAttribute(k):
			bootstrapConfigAttrs[k] = v
			continue
		case controller.ControllerOnlyAttribute(k):
			controllerConfigAttrs[k] = v
			continue
		}
		inheritedControllerAttrs[k] = v
	}
	// Region config values, for the region to be bootstrapped, from clouds.yaml
	// override what is in the cloud config.
	for k, v := range cloud.RegionConfig[c.Region] {
		switch {
		case bootstrap.IsBootstrapAttribute(k):
			bootstrapConfigAttrs[k] = v
			continue
		case controller.ControllerOnlyAttribute(k):
			controllerConfigAttrs[k] = v
			continue
		}
		inheritedControllerAttrs[k] = v
	}

	// Model defaults are added to the inherited controller attributes.
	// Any command line set model defaults override what is in the cloud config.
	for k, v := range modelDefaultConfigAttrs {
		switch {
		case bootstrap.IsBootstrapAttribute(k):
			return bootstrapConfigs{},
				errors.Errorf("%q is a bootstrap only attribute, and cannot be set as a model-default", k)
		case controller.ControllerOnlyAttribute(k):
			return bootstrapConfigs{},
				errors.Errorf("%q is a controller attribute, and cannot be set as a model-default", k)
		}
		inheritedControllerAttrs[k] = v
	}

	// Start with the model defaults, then add in user config attributes.
	maps.Copy(combinedConfig, modelDefaultConfigAttrs)

	// Store specific attributes are either already specified in model
	// config (but may have been coerced), or were not present. Either way,
	// copy them in.
	logger.Debugf(context.TODO(), "provider attrs: %v", providerAttrs)
	maps.Copy(combinedConfig, providerAttrs)

	maps.Copy(combinedConfig, inheritedControllerAttrs)

	maps.Copy(combinedConfig, userConfigAttrs)

	// Add in any default attribute values if not already
	// specified, making the recorded bootstrap config
	// immutable to changes in Juju.
	for k, v := range config.ConfigDefaults() {
		if _, ok := combinedConfig[k]; !ok {
			combinedConfig[k] = v
		}
	}

	bootstrapModelConfig := make(map[string]any)
	for k, v := range combinedConfig {
		switch {
		case bootstrap.IsBootstrapAttribute(k):
			bootstrapConfigAttrs[k] = v
		case controller.ControllerOnlyAttribute(k):
			controllerConfigAttrs[k] = v
		default:
			bootstrapModelConfig[k] = v
		}
	}

	bootstrapConfig, err := bootstrap.NewConfig(bootstrapConfigAttrs)
	if err != nil {
		return bootstrapConfigs{}, errors.Annotate(err, "constructing bootstrap config")
	}

	// Pre-process controller attributes.
	if _, ok := controllerConfigAttrs[controller.CAASOperatorImagePath]; ok {
		return bootstrapConfigs{}, fmt.Errorf("%q is no longer supported controller configuration",
			controller.CAASOperatorImagePath)
	}
	if _, ok := controllerConfigAttrs[controller.SystemSSHKeys]; ok {
		return bootstrapConfigs{}, fmt.Errorf("%q is not a user configurable item. Please unset this and continue.", controller.SystemSSHKeys)
	}
	if v, ok := controllerConfigAttrs[controller.CAASImageRepo]; ok {
		if v, ok := v.(string); ok {
			repoDetails, err := docker.LoadImageRepoDetails(v)
			if err != nil {
				return bootstrapConfigs{}, errors.Annotatef(err, "processing %s", controller.CAASImageRepo)
			}
			controllerConfigAttrs[controller.CAASImageRepo] = repoDetails.Content()
		}
	}

	controllerConfig, err := controller.NewConfig(
		controllerUUID.String(),
		bootstrapConfig.CACert,
		controllerConfigAttrs,
	)
	if err != nil {
		return bootstrapConfigs{}, errors.Annotate(err, "constructing controller config")
	}
	if controllerConfig.AutocertDNSName() != "" {
		if _, ok := controllerConfigAttrs[controller.APIPort]; !ok {
			// The configuration did not explicitly mention the API port,
			// so default to 443 because it is not usually possible to
			// obtain autocert certificates without listening on port 443.
			controllerConfig[controller.APIPort] = 443
		}
	}

	logger.Debugf(context.TODO(), "preparing controller with config: %v", bootstrapModelConfig)

	configs := bootstrapConfigs{
		bootstrapModel:           bootstrapModelConfig,
		controller:               controllerConfig,
		bootstrap:                bootstrapConfig,
		inheritedControllerAttrs: inheritedControllerAttrs,
		userConfigAttrs:          userConfigAttrs,
		storagePools:             storagePools,
	}
	return configs, nil
}

// runInteractive queries the user about bootstrap config interactively at the
// command prompt.
func (c *controllerProvisioner) runInteractive(ctx *cmd.Context) error {
	scanner := bufio.NewScanner(ctx.Stdin)
	clouds, err := assembleClouds()
	if err != nil {
		return errors.Trace(err)
	}
	c.Cloud, err = queryCloud(clouds, lxdnames.DefaultCloud, scanner, ctx.Stdout)
	if err != nil {
		return errors.Trace(err)
	}
	cloud, err := common.CloudByName(c.Cloud)
	if err != nil {
		return errors.Trace(err)
	}

	switch len(cloud.Regions) {
	case 0:
		// No region to choose, nothing to do.
	case 1:
		// If there's just one, don't prompt, just use it.
		c.Region = cloud.Regions[0].Name
	default:
		c.Region, err = queryRegion(c.Cloud, cloud.Regions, scanner, ctx.Stdout)
		if err != nil {
			return errors.Trace(err)
		}
	}

	defName := defaultControllerName(c.Cloud, c.Region)

	name, err := queryName(defName, scanner, ctx.Stdout)
	c.setControllerName(name)
	if err != nil {
		return errors.Trace(err)
	}
	return nil
}

// checkProviderType ensures the provider type is okay.
func checkProviderType(envType string) error {
	featureflag.SetFlagsFromEnvironment(osenv.JujuFeatureFlagEnvKey)
	flag, ok := provisionalProviders[envType]
	if ok && !featureflag.Enabled(flag) {
		msg := `the %q provider is provisional in this version of Juju. To use it anyway, set JUJU_DEV_FEATURE_FLAGS="%s" in your shell model`
		return errors.Errorf(msg, envType, flag)
	}
	return nil
}

// handleBootstrapError is called to clean up if bootstrap fails.
func handleBootstrapError(ctx *cmd.Context, cleanup func() error) {
	ch := make(chan os.Signal, 1)
	ctx.InterruptNotify(ch)
	defer ctx.StopInterruptNotify(ch)
	defer close(ch)
	go func() {
		for range ch {
			// Newline prefix is intentional, so output appears as
			// "^C\nCtrl-C pressed" instead of "^CCtrl-C pressed".
			_, _ = fmt.Fprintln(ctx.GetStderr(), "\nCtrl-C pressed, cleaning up failed bootstrap")
		}
	}()
	logger.Debugf(context.TODO(), "cleaning up after failed bootstrap")
	if err := cleanup(); err != nil {
		logger.Errorf(context.TODO(), "error cleaning up: %v", err)
	}
}

func handleChooseCloudRegionError(ctx *cmd.Context, err error) error {
	if !common.IsChooseCloudRegionError(err) {
		return err
	}
	_, _ = fmt.Fprintf(ctx.GetStderr(),
		"%s\n\nSpecify an alternative region, or try %q.\n",
		err, "juju update-public-clouds",
	)
	return cmd.ErrSilent
}

func newStringIfNonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
