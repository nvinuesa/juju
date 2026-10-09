// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package kubernetes

import (
	"context"
	"strings"

	"github.com/juju/juju/core/arch"
	"github.com/juju/juju/core/version"
	"github.com/juju/juju/domain/deployment/charm"
	"github.com/juju/juju/internal/cloudconfig/podcfg"
	"github.com/juju/juju/internal/docker"
	"github.com/juju/juju/internal/docker/registry"
	"github.com/juju/juju/internal/errors"
)

// pinControllerImages selects both images before provisioning. Subsequent pod
// recreation uses the same manifests even if the registry tags change.
func pinControllerImages(ctx context.Context, cfg *podcfg.ControllerPodConfig) error {
	details, err := docker.NewImageRepoDetails(cfg.Controller.CAASImageRepo())
	if err != nil {
		return errors.Capture(err)
	}
	agentImage, err := cfg.GetControllerImagePath()
	if err != nil {
		return errors.Capture(err)
	}
	architecture := arch.DefaultArchitecture
	if cfg.Bootstrap.BootstrapMachineConstraints.HasArch() {
		architecture = *cfg.Bootstrap.BootstrapMachineConstraints.Arch
	}
	agentImage, err = registry.ResolveReference(ctx, agentImage, details, architecture)
	if err != nil {
		return errors.Errorf("pinning controller agent image: %w", err)
	}
	charmImage := cfg.CharmImage
	if charmImage == "" {
		charmBase := cfg.CharmBase
		if charmBase.Empty() {
			charmBase = version.DefaultSupportedLTSBase()
		}
		charmImage, err = podcfg.ImageForBase(details.Repository, charm.Base{
			Name: strings.ToLower(charmBase.OS), Channel: charm.Channel{Track: charmBase.Channel.Track, Risk: charm.Stable},
		})
		if err != nil {
			return errors.Capture(err)
		}
	}
	charmImage, err = registry.ResolveReference(ctx, charmImage, details, architecture)
	if err != nil {
		return errors.Errorf("pinning controller charm image: %w", err)
	}
	cfg.AgentImage, cfg.CharmImage = agentImage, charmImage
	return nil
}
