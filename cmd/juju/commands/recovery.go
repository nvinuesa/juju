// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package commands

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"

	jujuclock "github.com/juju/clock"
	"github.com/juju/errors"
	"github.com/juju/gnuflag"
	"github.com/juju/names/v6"

	"github.com/juju/juju/api/jujuclient"
	caas "github.com/juju/juju/caas"
	jujucloud "github.com/juju/juju/cloud"
	jujucmd "github.com/juju/juju/cmd"
	"github.com/juju/juju/cmd/cmd"
	"github.com/juju/juju/cmd/juju/common"
	"github.com/juju/juju/cmd/modelcmd"
	"github.com/juju/juju/core/instance"
	jujuversion "github.com/juju/juju/core/version"
	"github.com/juju/juju/domain/recovery"
	"github.com/juju/juju/environs"
	internalrecovery "github.com/juju/juju/internal/recovery"
)

func newRecoveryCommand() cmd.Command {
	c := &recoveryCommand{}
	c.CanClearCurrentModel = true
	return modelcmd.Wrap(c, modelcmd.WrapSkipModelFlags, modelcmd.WrapSkipDefaultModel)
}

type recoveryCommand struct {
	modelcmd.ModelCommandBase
	archivePath string
	sha256      string
}

func (c *recoveryCommand) Info() *cmd.Info {
	return jujucmd.Info(&cmd.Info{
		Name: "recovery", Args: "<backup-file>", Purpose: "Recover a controller from a backup archive.",
		Doc: `Recover a replacement controller using the cloud, region and controller name
recorded in the archive. The controller UUID, model UUIDs and controller CA are
preserved. Recovery requires the same Juju agent version as the backup.

Fence the source controller before recovery. If its name is registered on this
client, run juju unregister first. Kubernetes recovery requires the original
cluster and surviving workload namespaces.

The archive checksum is required. Local credentials are tried first; if none
are available, the archived controller model credential is used. An ambiguous
credential selection or authentication failure stops recovery.
`,
		Examples: "    juju recovery juju-backup.tar.gz --sha256 <checksum>",
		SeeAlso:  []string{"bootstrap", "create-backup", "unregister"},
	})
}

func (c *recoveryCommand) SetFlags(f *gnuflag.FlagSet) {
	c.ModelCommandBase.SetFlags(f)
	f.StringVar(&c.sha256, "sha256", "", "SHA-256 checksum of the backup archive (64 hexadecimal characters)")
}

func (c *recoveryCommand) Init(args []string) error {
	if len(args) == 0 {
		return errors.New("a backup archive is required")
	}
	if err := cmd.CheckEmpty(args[1:]); err != nil {
		return err
	}
	if c.sha256 == "" {
		return errors.New("--sha256 is required")
	}
	if err := validateRecoverySHA256(c.sha256); err != nil {
		return err
	}
	c.archivePath = args[0]
	return nil
}

func (c *recoveryCommand) Run(ctx *cmd.Context) error {
	info, err := c.preflight(ctx)
	if err != nil {
		return err
	}
	if _, err := c.ClientStore().ControllerByName(info.ControllerName); err == nil {
		return errors.Errorf("controller %q is registered locally; run juju unregister %s before recovery", info.ControllerName, info.ControllerName)
	} else if !errors.Is(err, errors.NotFound) {
		return err
	}
	version := jujuversion.Current
	channel, err := parseControllerCharmChannel(fmt.Sprintf("%d.%d/stable", version.Major, version.Minor))
	if err != nil {
		return err
	}
	p := controllerProvisioner{
		ModelCommandBase: c.ModelCommandBase,
		clock:            jujuclock.WallClock,
		Cloud:            info.CloudName, Region: info.Region, controllerName: info.ControllerName,
		AgentVersion: &version, ControllerCharmChannel: channel,
		RecoveryPath: ctx.AbsPath(c.archivePath), RecoverySHA256: c.sha256, recoveryInfo: info,
	}
	return p.provisionController(ctx)
}

func (c *recoveryCommand) preflight(ctx *cmd.Context) (*recovery.ArchiveInfo, error) {
	archivePath := c.archivePath
	if !filepath.IsAbs(archivePath) {
		archivePath = ctx.AbsPath(archivePath)
	}
	info, err := internalrecovery.ValidateArchive(ctx.Context, archivePath, c.sha256)
	if err != nil {
		return nil, errors.Annotate(err, "invalid recovery archive")
	}
	if err := info.CheckAgentVersion(jujuversion.Current); err != nil {
		return nil, err
	}
	if _, err := info.ModelFamily(); err != nil {
		return nil, err
	}
	if info.ControllerName == "" {
		return nil, errors.New("recovery archive does not record the source controller name")
	}
	if !names.IsValidCloud(info.CloudName) {
		return nil, errors.NotValidf("archived cloud name %q", info.CloudName)
	}
	return info, nil
}

func recoveryCloud(info *recovery.ArchiveInfo) jujucloud.Cloud {
	source := info.Cloud
	cloud := jujucloud.Cloud{
		Name: source.Name, Type: source.Type, Endpoint: source.Endpoint,
		IdentityEndpoint: source.IdentityEndpoint, StorageEndpoint: source.StorageEndpoint,
		CACertificates: source.CACertificates, SkipTLSVerify: source.SkipTLSVerify,
	}
	for _, auth := range source.AuthTypes {
		cloud.AuthTypes = append(cloud.AuthTypes, jujucloud.AuthType(auth))
	}
	for _, region := range source.Regions {
		endpoint, identityEndpoint, storageEndpoint := region.Endpoint, region.IdentityEndpoint, region.StorageEndpoint
		if endpoint == "" {
			endpoint = cloud.Endpoint
		}
		if identityEndpoint == "" {
			identityEndpoint = cloud.IdentityEndpoint
		}
		if storageEndpoint == "" {
			storageEndpoint = cloud.StorageEndpoint
		}
		cloud.Regions = append(cloud.Regions, jujucloud.Region{
			Name: region.Name, Endpoint: endpoint, IdentityEndpoint: identityEndpoint, StorageEndpoint: storageEndpoint,
		})
	}
	return cloud
}

func (c *controllerProvisioner) recoveryCredentials(ctx *cmd.Context, provider environs.EnvironProvider, cloud jujucloud.Cloud) (bootstrapCredentials, string, error) {
	credential, name, detected, err := c.localRecoveryCredential(ctx, provider, cloud)
	if err != nil {
		return bootstrapCredentials{}, "", errors.Annotate(err, "resolving local recovery credentials; configure a default with juju default-credential")
	}
	if credential != nil {
		ctx.Infof("Using local credentials for cloud %q", cloud.Name)
		creds := bootstrapCredentials{credential: credential, name: name}
		if detected {
			creds.detectedName, creds.name = name, ""
		}
		return creds, c.recoveryInfo.Region, nil
	}
	source := c.recoveryInfo.Credential
	if source == nil {
		return bootstrapCredentials{}, "", errors.New("no local or archived controller credential is available; use juju add-credential")
	}
	if source.Revoked || source.Invalid {
		return bootstrapCredentials{}, "", errors.New("archived controller credential is revoked or invalid; configure local credentials")
	}
	if !names.IsValidCloudCredentialName(source.Name) {
		return bootstrapCredentials{}, "", errors.NotValidf("archived credential name %q", source.Name)
	}
	archived := jujucloud.NewCredential(jujucloud.AuthType(source.AuthType), source.Attributes)
	region, err := common.ChooseCloudRegion(cloud, c.recoveryInfo.Region)
	if err != nil {
		return bootstrapCredentials{}, "", err
	}
	credential, err = provider.FinalizeCredential(ctx, environs.FinalizeCredentialParams{
		Credential: archived, CloudName: cloud.Name, CloudEndpoint: region.Endpoint,
		CloudIdentityEndpoint: region.IdentityEndpoint, CloudStorageEndpoint: region.StorageEndpoint,
	})
	if err != nil {
		return bootstrapCredentials{}, "", errors.Annotate(err, "finalising archived recovery credential")
	}
	ctx.Infof("Using archived controller credentials for cloud %q", cloud.Name)
	return bootstrapCredentials{credential: credential, name: source.Name}, c.recoveryInfo.Region, nil
}

// Select a credential before finalising it. A failure while processing a
// selected credential must never be mistaken for an absent credential.
func (c *controllerProvisioner) localRecoveryCredential(ctx *cmd.Context, provider environs.EnvironProvider, cloud jujucloud.Cloud) (*jujucloud.Credential, string, bool, error) {
	stored, err := c.ClientStore().CredentialForCloud(cloud.Name)
	if err != nil && !errors.Is(err, errors.NotFound) {
		return nil, "", false, err
	}
	detected := false
	if err != nil || len(stored.AuthCredentials) == 0 {
		stored, err = provider.DetectCredentials(cloud.Name)
		if errors.Is(err, errors.NotFound) {
			return nil, "", false, nil
		}
		if err != nil {
			return nil, "", false, err
		}
		if stored == nil || len(stored.AuthCredentials) == 0 {
			return nil, "", false, nil
		}
		detected = true
	}
	name := stored.DefaultCredential
	if name == "" {
		if len(stored.AuthCredentials) > 1 {
			return nil, "", false, modelcmd.ErrMultipleCredentials
		}
		for key := range stored.AuthCredentials {
			name = key
		}
	}
	selected, ok := stored.AuthCredentials[name]
	if !ok {
		return nil, "", false, errors.Errorf("local default credential %q is unavailable", name)
	}
	if !names.IsValidCloudCredentialName(name) {
		return nil, "", false, errors.NotValidf("local credential name %q", name)
	}
	credential, err := modelcmd.FinalizeFileContent(&selected, provider)
	if err != nil {
		return nil, "", false, err
	}
	region, err := common.ChooseCloudRegion(cloud, c.recoveryInfo.Region)
	if err != nil {
		return nil, "", false, err
	}
	credential, err = provider.FinalizeCredential(ctx, environs.FinalizeCredentialParams{
		Credential: *credential, CloudName: cloud.Name, CloudEndpoint: region.Endpoint,
		CloudIdentityEndpoint: region.IdentityEndpoint, CloudStorageEndpoint: region.StorageEndpoint,
	})
	return credential, name, detected, err
}

// validateRecoverySHA256 enforces the operator-supplied checksum format:
// exactly 64 hexadecimal characters. A malformed checksum can never match,
// so fail before the preflight reads the archive.
func validateRecoverySHA256(sum string) error {
	decoded, err := hex.DecodeString(sum)
	if err != nil || len(decoded) != sha256.Size {
		return errors.New("--sha256 must be a SHA-256 checksum: 64 hexadecimal characters")
	}
	return nil
}

// cleanupFailedRecovery tears down the replacement created by recovery.
// The replacement uses the source controller and controller model UUIDs,
// so a full
// environ or controller destroy matches the fenced source controller's
// machines and the surviving workload substrate by tag and must never
// run here.
//
// On Kubernetes the environ destroy is model-scoped: it deletes only the
// freshly created controller namespace. On machine clouds the cleanup
// stops exactly the replacement instance when the failure happened after
// it started; a failure before that leaves nothing to remove. Env-level
// resources created for the replacement (security groups, LXD profiles,
// storage) may remain.
//
// startedInstanceID is the replacement's instance id stashed by the
// controller provisioner once the instance was running; it covers failures
// after that point whose error carries no instance id.
func cleanupFailedRecovery(
	controllerName string,
	environ environs.BootstrapEnviron,
	resultErr error,
	startedInstanceID instance.Id,
	ctx *cmd.Context,
	store jujuclient.ControllerStore,
) error {
	switch {
	case isK8sRecoveryEnviron(environ):
		// The Kubernetes destroy is model-scoped: it deletes the replacement's
		// own controller namespace, not the surviving workload
		// namespaces.
		ctx.Infof("Destroying the replacement controller namespace")
		if err := environ.Destroy(ctx); err != nil {
			return errors.Annotate(err, "destroying replacement controller namespace")
		}
	default:
		id, ok := environs.BootstrapInstanceID(resultErr)
		if !ok {
			if startedInstanceID == "" {
				// The failure happened before any instance was
				// started: there is no cloud resource of this
				// bootstrap to remove.
				ctx.Infof("Recovery failed before the replacement instance started: no cloud resources to clean up")
				break
			}
			// A late failure after the instance started: the error
			// carries no instance id, but the bootstrap stashed the
			// replacement's.
			id = startedInstanceID
			ctx.Infof("Replacement instance still running, stopping it")
		}
		broker, ok := environ.(environs.InstanceBroker)
		if !ok {
			return errors.NotSupportedf("stopping instance %s on provider %T", id, environ)
		}
		ctx.Infof("Stopping the replacement instance %s", id)
		if err := broker.StopInstances(ctx, id); err != nil {
			return errors.Annotatef(err, "stopping failed recovery instance %s", id)
		}
		ctx.Infof(
			"Environment-level resources created for the replacement (security groups, LXD profiles, storage) may remain and can be removed manually")
	}
	if err := store.RemoveController(controllerName); err != nil && !errors.Is(err, errors.NotFound) {
		return errors.Trace(err)
	}
	return nil
}

// isK8sRecoveryEnviron reports whether the environ is a Kubernetes
// controller environ.
func isK8sRecoveryEnviron(environ environs.BootstrapEnviron) bool {
	_, ok := environ.(caas.ServiceManager)
	return ok
}

// recoveryModels maps the archived model inventory to the provider-facing
// recovery model references, including the Kubernetes workload inventory the
// provider substrate check verifies.
func recoveryModels(info *recovery.ArchiveInfo) []environs.RecoveryModel {
	models := make([]environs.RecoveryModel, len(info.Models))
	for i, m := range info.Models {
		apps := make([]environs.RecoveryApplication, len(m.Applications))
		for j, app := range m.Applications {
			apps[j] = environs.RecoveryApplication{
				Name:                   app.Name,
				UUID:                   app.UUID,
				Units:                  app.Units,
				PersistentVolumeClaims: app.FilesystemProviderIDs,
			}
		}
		models[i] = environs.RecoveryModel{Name: m.Name, UUID: m.UUID, Applications: apps}
	}
	return models
}

// printRecoverySummary reports the recovered identities, the missing
// substrate the surviving target no longer holds, and the immediate
// reconciliation effects of the recovery. The agent-side load reports
// the database-derived details (dead controller machines, pending
// removals) in the controller log.
func (c *controllerProvisioner) printRecoverySummary(
	ctx *cmd.Context,
	info *recovery.ArchiveInfo,
	report *environs.RecoverySubstrateReport,
) {
	fmt.Fprintf(ctx.Stdout, `
Recovery complete
  controller:      %s (%s)
  agent version:   %s (backup finished %s)
  models recovered: %d
`, info.ControllerUUID, info.ControllerName,
		info.AgentVersion, info.BackupFinished.Format("2006-01-02 15:04:05"),
		len(info.Models))
	if info.HANodes > 1 {
		fmt.Fprintf(ctx.Stdout, `  HA source:       %d nodes; the other controller machines loaded as dead machines and must be removed
`, info.HANodes)
	}
	if !report.Empty() {
		fmt.Fprintf(ctx.Stdout, `
Missing workload substrate (reported before provisioning; the first
reconcile recreates workload objects, and a recreated volume comes
back empty):
`)
		for _, workload := range report.MissingWorkloads {
			fmt.Fprintf(ctx.Stdout, "  missing workload objects: %s\n", workload)
		}
		for _, claim := range report.MissingPersistentVolumeClaims {
			fmt.Fprintf(ctx.Stdout, "  missing volume (recreated empty): %s\n", claim)
		}
	}
	fmt.Fprintf(ctx.Stdout, `
The controller reconciles immediately: pending removals, dying entities and
recorded operations from the backup now run. Check the controller log for the
database-side recovery summary and review pending work before continuing.
`)
}
