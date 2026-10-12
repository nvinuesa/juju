// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package recovery

import (
	"context"
	"os"
	"sort"

	"github.com/juju/juju/core/logger"
	coremodel "github.com/juju/juju/core/model"
	domainrecovery "github.com/juju/juju/domain/recovery"
	"github.com/juju/juju/internal/errors"
)

// RecoveryService provides persistence behind the domain service boundary.
// It operates within an exclusive bootstrap database session. Loads commit
// independently; a later failure requires discarding the target.
type RecoveryService interface {
	LoadControllerDump(context.Context, []byte) error
	LoadModelDump(context.Context, coremodel.UUID, []byte) error
	GetObjectInventory(context.Context, []coremodel.UUID) ([]domainrecovery.ObjectInventory, error)
	PatchControllerMachine(context.Context, coremodel.UUID, domainrecovery.MachinePatch) (string, error)
	PatchControllerUnitAddresses(context.Context, coremodel.UUID, []string) error
	BuildSummary(context.Context, *domainrecovery.ArchiveInfo, []coremodel.UUID, string, int) (*domainrecovery.Summary, error)
}

// LoadParams carries everything the recovery load stage needs. It runs
// inside bootstrap, after Dqlite is initialised and before any agent, API
// or worker starts; the stage owns every database exclusively.
type LoadParams struct {
	// Service is fully constructed before recovery begins.
	Service RecoveryService

	// ArchivePath is the uploaded archive on the target's disk.
	ArchivePath string

	// ExpectedSHA256 is the operator-supplied archive checksum (hex).
	ExpectedSHA256 string

	// ObjectsArchiveDir is private staging for the unpacked object files.
	ObjectsArchiveDir string

	// DataDir is the replacement's data directory; blobs install into
	// its objectstore tree.
	DataDir string

	// ControllerModelUUID identifies the controller model; its database
	// receives the machine patch.
	ControllerModelUUID string

	// ControllerUnitAddresses carries the replacement's controller-unit
	// addresses (on Kubernetes, the stable per-ordinal controller pod
	// FQDNs). The unit's IP and DNS observations are replaced before the
	// api-address-setter can publish addresses from the archive.
	ControllerUnitAddresses []string

	// MachinePatch carries the replacement machine's physical facts.
	// Nil on Kubernetes, where there is no machine to patch.
	MachinePatch *domainrecovery.MachinePatch

	Logger logger.Logger
}

// Load executes the recovery stage: validate the uploaded archive, load
// the controller database, install the object blobs, load every model
// database and patch the replacement's physical facts in place. Any
// failure fails bootstrap; the target is discarded and bootstrap re-run.
func Load(ctx context.Context, params LoadParams) (*domainrecovery.Summary, error) {
	contents, info, err := ReadArchive(ctx, params.Logger, params.ArchivePath, params.ExpectedSHA256)
	if err != nil {
		return nil, errors.Capture(err)
	}
	defer func() { _ = contents.Close() }()
	params.Logger.Infof(ctx, "recovering controller %q from backup finished at %s",
		info.ControllerUUID, info.BackupFinished)

	if params.Service == nil {
		return nil, errors.New("missing recovery service")
	}
	if err := params.Service.LoadControllerDump(ctx, contents.ControllerDump); err != nil {
		return nil, errors.Capture(err)
	}

	modelUUIDs := make([]string, 0, len(contents.ModelDumps))
	for modelUUID := range contents.ModelDumps {
		modelUUIDs = append(modelUUIDs, modelUUID)
	}
	sort.Strings(modelUUIDs)

	models := make([]coremodel.UUID, 0, len(modelUUIDs))
	controllerModelFound := false
	for _, modelUUID := range modelUUIDs {
		if err := ctx.Err(); err != nil {
			return nil, errors.Capture(err)
		}
		data, err := os.ReadFile(contents.ModelDumps[modelUUID])
		if err != nil {
			return nil, errors.Errorf("reading dump for model %q: %w", modelUUID, err)
		}
		model := coremodel.UUID(modelUUID)
		if err := params.Service.LoadModelDump(ctx, model, data); err != nil {
			return nil, errors.Capture(err)
		}
		models = append(models, model)
		if modelUUID == params.ControllerModelUUID {
			controllerModelFound = true
		}
		params.Logger.Tracef(ctx, "loaded database for model %s", modelUUID)
	}
	if !controllerModelFound {
		return nil, errors.Errorf("controller model %q has no database dump", params.ControllerModelUUID)
	}

	// Install the archived object blobs after every database is loaded:
	// object metadata lives in the model databases too.
	if err := UnpackObjectsArchive(ctx, params.ArchivePath, params.ObjectsArchiveDir); err != nil {
		return nil, errors.Capture(err)
	}
	inventories, err := params.Service.GetObjectInventory(ctx, models)
	if err != nil {
		return nil, errors.Capture(err)
	}
	copied, err := LoadObjects(ctx, inventories, params.ObjectsArchiveDir, params.DataDir)
	if err != nil {
		return nil, errors.Capture(err)
	}
	params.Logger.Infof(ctx, "installed %d object store blobs", copied)

	// patchTarget is the archived machine the replacement maps onto:
	// the live controller machine resolved from the loaded dump, never
	// an assumed name.
	var patchTarget string
	if params.MachinePatch != nil {
		patchTarget, err = params.Service.PatchControllerMachine(ctx,
			coremodel.UUID(params.ControllerModelUUID), *params.MachinePatch)
		if err != nil {
			return nil, errors.Capture(err)
		}
	}

	if len(params.ControllerUnitAddresses) > 0 {
		// Synchronise the unit's IP and DNS observations with the
		// replacement before workers can publish them.
		if err := params.Service.PatchControllerUnitAddresses(ctx,
			coremodel.UUID(params.ControllerModelUUID), params.ControllerUnitAddresses); err != nil {
			return nil, errors.Capture(err)
		}
	}

	summary, err := params.Service.BuildSummary(ctx, info, models, patchTarget, copied)
	if err != nil {
		return nil, errors.Capture(err)
	}

	return summary, nil
}
