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
	"github.com/juju/juju/cmd/modelcmd"
	"github.com/juju/juju/domain/recovery"
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
