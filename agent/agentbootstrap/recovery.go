// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package agentbootstrap

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"time"

	coredatabase "github.com/juju/juju/core/database"
	"github.com/juju/juju/core/instance"
	coremodel "github.com/juju/juju/core/model"
	domainrecovery "github.com/juju/juju/domain/recovery"
	"github.com/juju/juju/internal/cloudconfig/instancecfg"
	"github.com/juju/juju/internal/database"
	"github.com/juju/juju/internal/database/app"
	"github.com/juju/juju/internal/errors"
	"github.com/juju/juju/internal/recovery"
)

// recoveryPollInterval is how long the agent waits between checks for the
// uploaded recovery archive. The archive is uploaded by the bootstrap
// client over SSH (IAAS) or the Kubernetes API (CAAS) while the agent is
// starting, so a short poll tolerates the race.
const recoveryPollInterval = 2 * time.Second

// recoveryArchiveWaitTimeout bounds the wait for the uploaded recovery
// archive: a lost upload must fail bootstrap with a clear error instead of
// hanging until the bootstrap-wide timeout.
var recoveryArchiveWaitTimeout = 15 * time.Minute

// waitForRecoveryArchive blocks until the recovery archive upload lands on
// disk, the context is cancelled, the wait timeout expires, or stat fails
// unexpectedly.
func (b *AgentBootstrap) waitForRecoveryArchive(ctx context.Context, archivePath string) error {
	deadline := b.clock.Now().Add(recoveryArchiveWaitTimeout)
	for {
		if _, err := os.Stat(archivePath); err == nil {
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return errors.Errorf("checking recovery archive %q: %w", archivePath, err)
		}
		b.logger.Infof(ctx, "waiting for recovery archive upload at %s", archivePath)
		select {
		case <-ctx.Done():
			return errors.Capture(ctx.Err())
		case <-b.clock.After(recoveryPollInterval):
		}
		if b.clock.Now().After(deadline) {
			return errors.Errorf(
				"recovery archive did not appear at %q within %s; the upload may have failed and bootstrap must be re-run",
				archivePath, recoveryArchiveWaitTimeout)
		}
	}
}

// recoveryLoadStage builds the recovery stage run inside the dqlite
// bootstrap, right after the schema migrations and before the app is
// closed. At this point bootstrap owns every database exclusively: no
// agent, API or worker has started.
func (b *AgentBootstrap) recoveryLoadStage(
	stateParams instancecfg.StateInitializationParams,
	controllerModelUUID coremodel.UUID,
) database.RecoveryStage {
	return func(ctx context.Context, dqlite *app.App) error {
		controllerDB, err := dqlite.Open(ctx, coredatabase.ControllerNS)
		if err != nil {
			return errors.Errorf("opening controller database: %w", err)
		}
		defer func() { _ = controllerDB.Close() }()

		bundleDir := filepath.Join(b.agentConfig.DataDir(), "recovery", "bundle")
		machinePatch := recoveryMachinePatch(stateParams)
		var unitAddresses []string
		if machinePatch == nil {
			// Kubernetes: no machine to patch; the replacement's
			// controller pod FQDNs patch the controller unit's
			// addresses instead.
			unitAddresses = b.bootstrapMachineAddresses.Values()
		}
		summary, err := recovery.Load(ctx, recovery.LoadParams{
			ControllerDB: controllerDB,
			OpenModelDB: func(ctx context.Context, modelUUID string) (*sql.DB, error) {
				return database.EnsureModelDatabase(ctx, dqlite, modelUUID, b.logger)
			},
			ArchivePath:             stateParams.RecoveryArchivePath,
			ExpectedSHA256:          stateParams.RecoverySHA256,
			BundleDir:               bundleDir,
			DataDir:                 b.agentConfig.DataDir(),
			ControllerModelUUID:     controllerModelUUID.String(),
			MachinePatch:            machinePatch,
			ControllerUnitAddresses: unitAddresses,
			Logger:                  b.logger,
		})
		if err != nil {
			return errors.Capture(err)
		}
		if err := os.RemoveAll(bundleDir); err != nil {
			b.logger.Warningf(ctx, "cleaning recovery staging: %v", err)
		}

		b.logger.Infof(ctx,
			"recovery complete: controller %q (%s), %d models, %d objects copied, "+
				"%d dead controller machines, %d non-alive machines, %d non-alive applications, "+
				"%d non-alive units, %d pending removals",
			summary.ControllerUUID, summary.ControllerName,
			summary.Models, summary.ObjectsCopied, len(summary.DeadControllerMachines),
			summary.MachinesNotAlive, summary.ApplicationsNotAlive,
			summary.UnitsNotAlive, summary.PendingRemovals)
		return nil
	}
}

// recoveryMachinePatch maps the replacement machine's physical facts from
// the bootstrap parameters. It returns nil on Kubernetes, where there is
// no machine to patch.
func recoveryMachinePatch(stateParams instancecfg.StateInitializationParams) *domainrecovery.MachinePatch {
	if stateParams.BootstrapMachineInstanceId == "" {
		return nil
	}
	// The machine name is resolved from the loaded dump (the archived
	// live controller machine) by the recovery stage: bootstrap knows
	// only the replacement's physical facts.
	patch := &domainrecovery.MachinePatch{
		InstanceID:  string(stateParams.BootstrapMachineInstanceId),
		DisplayName: stateParams.BootstrapMachineDisplayName,
	}
	if hw := stateParams.BootstrapMachineHardwareCharacteristics; hw != nil {
		applyHardware(patch, hw)
	}
	return patch
}

// applyHardware copies the observed hardware characteristics into the
// patch.
func applyHardware(patch *domainrecovery.MachinePatch, hw *instance.HardwareCharacteristics) {
	if hw.Arch != nil {
		patch.Arch = *hw.Arch
	}
	if hw.Mem != nil {
		patch.MemMB = *hw.Mem
	}
	if hw.CpuCores != nil {
		patch.Cores = *hw.CpuCores
	}
	if hw.RootDisk != nil {
		patch.RootDiskMB = *hw.RootDisk
	}
}
