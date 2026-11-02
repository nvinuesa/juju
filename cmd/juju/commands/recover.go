// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package commands

import (
	"path/filepath"

	"github.com/juju/errors"
	"github.com/juju/names/v6"

	jujucloud "github.com/juju/juju/cloud"
	jujucmd "github.com/juju/juju/cmd"
	"github.com/juju/juju/cmd/cmd"
	"github.com/juju/juju/cmd/juju/backups"
	"github.com/juju/juju/cmd/juju/common"
	"github.com/juju/juju/cmd/modelcmd"
	"github.com/juju/juju/domain/recovery"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/internal/docker"
	internalrecovery "github.com/juju/juju/internal/recovery"
)

func newRecoveryCommand() cmd.Command {
	c := &recoveryCommand{}
	c.CanClearCurrentModel = true
	return modelcmd.Wrap(c, modelcmd.WrapSkipModelFlags, modelcmd.WrapSkipDefaultModel)
}

type recoveryCommand struct {
	modelcmd.ModelCommandBase
	archivePath   string
	assertionPath string
	recoveryInfo  *recovery.ArchiveInfo
}

func (c *recoveryCommand) Info() *cmd.Info {
	return jujucmd.Info(&cmd.Info{
		Name: "recover", Args: "<backup-file> <assertion-file>",
		Purpose: "Validate a controller recovery archive.",
		Doc:     "Validate an archive against its unsigned SHA-256 assertion. Controller recovery provisioning is not available in this build.",
		SeeAlso: []string{"bootstrap", "create-backup", "unregister"},
	})
}

func (c *recoveryCommand) Init(args []string) error {
	switch {
	case len(args) == 0 || args[0] == "":
		return errors.New("a backup archive is required")
	case len(args) == 1 || args[1] == "":
		return errors.New("a backup assertion file is required")
	}
	if err := cmd.CheckEmpty(args[2:]); err != nil {
		return err
	}
	c.archivePath = args[0]
	c.assertionPath = args[1]
	return nil
}

func (c *recoveryCommand) Run(ctx *cmd.Context) error {
	if _, err := c.preflight(ctx); err != nil {
		return err
	}
	return errors.New("controller recovery provisioning is not available in this build")
}

// preflight validates the recovery archive and its assertion, and returns
// the archived controller identity for replacement planning.
func (c *recoveryCommand) preflight(ctx *cmd.Context) (*recovery.ArchiveInfo, error) {
	archivePath := c.archivePath
	if !filepath.IsAbs(archivePath) {
		archivePath = ctx.AbsPath(archivePath)
	}
	assertionPath := c.assertionPath
	if !filepath.IsAbs(assertionPath) {
		assertionPath = ctx.AbsPath(assertionPath)
	}
	assertion, err := c.readAssertion(assertionPath)
	if err != nil {
		return nil, err
	}
	info, err := internalrecovery.ValidateArchive(ctx.Context, logger, archivePath, assertion.Checksum)
	if err != nil {
		return nil, errors.Annotate(err, "invalid recovery archive")
	}
	if info.CloudType == "manual" {
		return nil, errors.NotSupportedf("automatic recovery provisioning on manual clouds")
	}
	if info.ControllerName == "" {
		return nil, errors.New("recovery archive does not record the source controller name")
	}
	if info.CloudType == jujucloud.CloudTypeKubernetes {
		if info.FormatVersion < 2 || info.Kubernetes == nil {
			return nil, errors.New("Kubernetes recovery requires source provisioning metadata; create a new backup")
		}
		if info.Kubernetes.Architecture != info.ControllerArchitecture || info.Kubernetes.StorageSizeMiB == 0 {
			return nil, errors.New("source Kubernetes provisioning disagrees with the archived controller")
		}
		if err := docker.ValidateDockerRegistryPath(info.Kubernetes.AgentImage); err != nil {
			return nil, errors.Annotate(err, "invalid archived controller agent image")
		}
		if err := docker.ValidateDockerRegistryPath(info.Kubernetes.CharmImage); err != nil {
			return nil, errors.Annotate(err, "invalid archived controller charm image")
		}
	}
	if !names.IsValidCloud(info.CloudName) {
		return nil, errors.NotValidf("archived cloud name %q", info.CloudName)
	}
	return info, nil
}

// readAssertion opens and parses the backup assertion file. Every open,
// read and close failure carries the assertion filename; a parse error
// wins over a close failure.
func (c *recoveryCommand) readAssertion(assertionPath string) (*backups.Assertion, error) {
	file, err := c.Filesystem().Open(assertionPath)
	if err != nil {
		return nil, errors.Annotatef(err, "reading backup assertion %q", assertionPath)
	}
	assertion, err := backups.ReadAssertion(file)
	closeErr := file.Close()
	if err != nil {
		return nil, errors.Annotatef(err, "reading backup assertion %q", assertionPath)
	}
	if closeErr != nil {
		return nil, errors.Annotatef(closeErr, "reading backup assertion %q", assertionPath)
	}
	return assertion, nil
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

func (c *recoveryCommand) recoveryCredentials(ctx *cmd.Context, provider environs.EnvironProvider, cloud jujucloud.Cloud) (bootstrapCredentials, string, error) {
	return c.recoveryCredentialsForCloud(ctx, provider, cloud, c.recoveryInfo.Region, c.recoveryInfo.Credential)
}

func (c *recoveryCommand) recoveryCredentialsForCloud(ctx *cmd.Context, provider environs.EnvironProvider, cloud jujucloud.Cloud, sourceRegion string, source *recovery.CredentialInfo) (bootstrapCredentials, string, error) {
	credential, name, detected, err := c.localRecoveryCredential(ctx, provider, cloud, sourceRegion)
	if err != nil {
		return bootstrapCredentials{}, "", errors.Annotate(err, "resolving local recovery credentials; configure a default with juju default-credential")
	}
	if credential != nil {
		ctx.Infof("Using local credentials for cloud %q", cloud.Name)
		creds := bootstrapCredentials{credential: credential, name: name}
		if detected {
			creds.detectedName, creds.name = name, ""
		}
		return creds, sourceRegion, nil
	}
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
	region, err := common.ChooseCloudRegion(cloud, sourceRegion)
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
	return bootstrapCredentials{credential: credential, name: source.Name}, sourceRegion, nil
}

// Select a credential before finalising it. A failure while processing a
// selected credential must never be mistaken for an absent credential.
func (c *recoveryCommand) localRecoveryCredential(ctx *cmd.Context, provider environs.EnvironProvider, cloud jujucloud.Cloud, sourceRegion string) (*jujucloud.Credential, string, bool, error) {
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
	region, err := common.ChooseCloudRegion(cloud, sourceRegion)
	if err != nil {
		return nil, "", false, err
	}
	credential, err = provider.FinalizeCredential(ctx, environs.FinalizeCredentialParams{
		Credential: *credential, CloudName: cloud.Name, CloudEndpoint: region.Endpoint,
		CloudIdentityEndpoint: region.IdentityEndpoint, CloudStorageEndpoint: region.StorageEndpoint,
	})
	return credential, name, detected, err
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
