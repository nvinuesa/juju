// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package commands

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/juju/errors"

	"github.com/juju/juju/api/jujuclient"
	caas "github.com/juju/juju/caas"
	jujucloud "github.com/juju/juju/cloud"
	"github.com/juju/juju/cmd/cmd"
	jujuversion "github.com/juju/juju/core/version"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/internal/restore"
)

// runRestorePreflight validates the restore archive offline and checks it
// against the target cloud, before anything is provisioned. It returns
// nil for a normal bootstrap.
func (c *bootstrapCommand) runRestorePreflight(ctx *cmd.Context, cloud jujucloud.Cloud) (*restore.ArchiveInfo, error) {
	if c.RestorePath == "" {
		return nil, nil
	}

	info, err := restore.ValidateArchive(ctx.Context, c.RestorePath, c.RestoreSHA256)
	if err != nil {
		return nil, errors.Annotate(err, "invalid restore archive")
	}
	if err := info.CheckAgentVersion(jujuversion.Current); err != nil {
		return nil, errors.Trace(err)
	}
	if _, err := info.ModelFamily(); err != nil {
		return nil, errors.Trace(err)
	}
	if err := info.CheckProviderFamily(cloud.Type); err != nil {
		return nil, errors.Trace(err)
	}
	return info, nil
}

// validateRestoreSHA256 enforces the operator-supplied checksum format:
// exactly 64 hexadecimal characters. A malformed checksum can never match,
// so fail before the preflight reads the archive.
func validateRestoreSHA256(sum string) error {
	decoded, err := hex.DecodeString(sum)
	if err != nil || len(decoded) != sha256.Size {
		return errors.New("--restore-sha256 must be a SHA-256 checksum: 64 hexadecimal characters")
	}
	return nil
}

// cleanupFailedRestoreBootstrap tears down what a failed restore
// bootstrap created, and nothing else. The replacement is provisioned
// with the source controller's and controller model's UUIDs, so a full
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
func cleanupFailedRestoreBootstrap(
	controllerName string,
	environ environs.BootstrapEnviron,
	resultErr error,
	ctx *cmd.Context,
	store jujuclient.ControllerStore,
) error {
	switch {
	case environIsCAAS(environ):
		// The CAAS destroy is model-scoped: it deletes the replacement's
		// own controller namespace, not the surviving workload
		// namespaces.
		ctx.Infof("Destroying the replacement controller namespace")
		if err := environ.Destroy(ctx); err != nil {
			return errors.Annotate(err, "destroying replacement controller namespace")
		}
	default:
		id, ok := environs.BootstrapInstanceID(resultErr)
		if !ok {
			// The failure happened before any instance was started:
			// there is no cloud resource of this bootstrap to remove.
			ctx.Infof("Bootstrap failed before the replacement instance started: no cloud resources to clean up")
			break
		}
		broker, ok := environ.(environs.InstanceBroker)
		if !ok {
			return errors.NotSupportedf("stopping instance %s on provider %T", id, environ)
		}
		ctx.Infof("Stopping the replacement instance %s", id)
		if err := broker.StopInstances(ctx, id); err != nil {
			return errors.Annotatef(err, "stopping failed bootstrap instance %s", id)
		}
		ctx.Infof(
			"Environment-level resources created for the replacement (security groups, LXD profiles, storage) may remain and can be removed manually")
	}
	if err := store.RemoveController(controllerName); err != nil && !errors.Is(err, errors.NotFound) {
		return errors.Trace(err)
	}
	return nil
}

// environIsCAAS reports whether the bootstrap environ is a Kubernetes
// controller environ.
func environIsCAAS(environ environs.BootstrapEnviron) bool {
	_, ok := environ.(caas.ServiceManager)
	return ok
}

// restoreModels maps the archived model inventory to the provider-facing
// restore model references.
func restoreModels(info *restore.ArchiveInfo) []environs.RestoreModel {
	models := make([]environs.RestoreModel, len(info.Models))
	for i, m := range info.Models {
		models[i] = environs.RestoreModel{Name: m.Name, UUID: m.UUID}
	}
	return models
}

// printRestoreSummary reports the restored identities and the immediate
// reconciliation effects of the restore. The agent-side load reports the
// database-derived details (dead controller machines, pending removals)
// in the controller log.
func (c *bootstrapCommand) printRestoreSummary(ctx *cmd.Context, info *restore.ArchiveInfo) {
	fmt.Fprintf(ctx.Stdout, `
Restore complete
  controller:      %s (%s)
  agent version:   %s (backup finished %s)
  models restored: %d
`, info.ControllerUUID, info.ControllerName,
		info.AgentVersion, info.BackupFinished.Format("2006-01-02 15:04:05"),
		len(info.Models))
	if info.HANodes > 1 {
		fmt.Fprintf(ctx.Stdout, `  HA source:       %d nodes; the other controller machines loaded as dead machines and must be removed
`, info.HANodes)
	}
	fmt.Fprintf(ctx.Stdout, `
The controller reconciles immediately: pending removals, dying entities and
recorded operations from the backup now run. Check the controller log for the
database-side restore summary and review pending work before continuing.
`)
}
