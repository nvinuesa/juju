// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package caas

import (
	"context"

	"github.com/juju/juju/environs"
	"github.com/juju/juju/internal/cloudconfig/instancecfg"
	"github.com/juju/juju/internal/cloudconfig/podcfg"
)

// RecoveryControllerParams describes a replacement controller deployment.
// PodConfig supplies recovery initialisation and an immutable agent image;
// Archive is transferred privately and verified before publication to the pod.
type RecoveryControllerParams struct {
	PodConfig *podcfg.ControllerPodConfig
	Archive   instancecfg.InitialisationFile
}

// RecoveryControllerProvisioner installs a replacement in a fresh namespace.
// When resources are created, it returns cleanup even on failure. Cleanup must
// use target ownership preconditions and leave surviving workload models alone.
// The caller invokes cleanup if provisioning or subsequent API login fails.
type RecoveryControllerProvisioner interface {
	ProvisionRecoveryController(environs.BootstrapContext, RecoveryControllerParams) (cleanup func(context.Context) error, err error)
}
