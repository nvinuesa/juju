// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package restore

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/juju/juju/core/logger"
	"github.com/juju/juju/internal/errors"
)

// LoadParams carries everything the restore load stage needs. It runs
// inside bootstrap, after Dqlite is initialized and before any agent, API
// or worker starts; the stage owns every database exclusively.
type LoadParams struct {
	// ControllerDB is the freshly bootstrapped controller database.
	ControllerDB *sql.DB

	// OpenModelDB opens the database for one model UUID, creating the
	// namespace and applying the model schema when it does not exist yet.
	OpenModelDB func(ctx context.Context, modelUUID string) (*sql.DB, error)

	// ArchivePath is the uploaded archive on the target's disk.
	ArchivePath string

	// ExpectedSHA256 is the operator-supplied archive checksum (hex).
	ExpectedSHA256 string

	// BundleDir is private staging for the unpacked object blob bundle.
	BundleDir string

	// DataDir is the replacement's data directory; blobs install into
	// its objectstore tree.
	DataDir string

	// ControllerModelUUID identifies the controller model; its database
	// receives the machine patch.
	ControllerModelUUID string

	// ControllerUnitAddresses carries the replacement's controller-unit
	// addresses (on Kubernetes, the stable per-ordinal controller pod
	// FQDNs). The archived controller unit's address rows are patched to
	// them: the source's pod IP is a dead physical fact that the
	// api-address-setter would republish to every agent as the
	// controller's API address.
	ControllerUnitAddresses []string

	// MachinePatch carries the replacement machine's physical facts.
	// Nil on Kubernetes, where there is no machine to patch.
	MachinePatch *MachinePatch

	Logger logger.Logger
}

// Load executes the restore stage: validate the uploaded archive, load
// the controller database, install the object blobs, load every model
// database and patch the replacement's physical facts in place. Any
// failure fails bootstrap; the target is discarded and bootstrap re-run.
func Load(ctx context.Context, params LoadParams) (*Summary, error) {
	contents, _, _, err := readArchive(ctx, params.ArchivePath, params.ExpectedSHA256)
	if err != nil {
		return nil, errors.Capture(err)
	}
	info, err := parseArchiveInfo(contents)
	if err != nil {
		return nil, errors.Capture(err)
	}
	params.Logger.Infof(ctx, "restoring controller %q from backup finished at %s",
		info.ControllerUUID, info.BackupFinished)

	controllerDump, err := DecodeDump(contents.controllerDump)
	if err != nil {
		return nil, errors.Capture(err)
	}
	if err := LoadControllerDump(ctx, params.ControllerDB, controllerDump); err != nil {
		return nil, errors.Capture(err)
	}

	modelUUIDs := make([]string, 0, len(contents.modelDumps))
	for modelUUID := range contents.modelDumps {
		modelUUIDs = append(modelUUIDs, modelUUID)
	}
	sort.Strings(modelUUIDs)

	modelDBs := make(map[string]*sql.DB, len(modelUUIDs))
	var controllerModelDB *sql.DB
	for _, modelUUID := range modelUUIDs {
		if err := ctx.Err(); err != nil {
			return nil, errors.Capture(err)
		}
		db, err := params.OpenModelDB(ctx, modelUUID)
		if err != nil {
			return nil, errors.Errorf("opening database for model %q: %w", modelUUID, err)
		}
		dump, err := DecodeDump(contents.modelDumps[modelUUID])
		if err != nil {
			return nil, errors.Errorf("model %q: %w", modelUUID, err)
		}
		if err := LoadModelDump(ctx, db, dump); err != nil {
			return nil, errors.Errorf("model %q: %w", modelUUID, err)
		}
		modelDBs[modelUUID] = db
		if modelUUID == params.ControllerModelUUID {
			controllerModelDB = db
		}
		params.Logger.Tracef(ctx, "loaded database for model %s", modelUUID)
	}
	if controllerModelDB == nil {
		return nil, errors.Errorf("controller model %q has no database dump", params.ControllerModelUUID)
	}

	// Install the archived object blobs after every database is loaded:
	// object metadata lives in the model databases too.
	if err := UnpackObjectsBundle(ctx, params.ArchivePath, params.BundleDir); err != nil {
		return nil, errors.Capture(err)
	}
	objectDBs := []ObjectDB{{Namespace: "controller", DB: params.ControllerDB}}
	for _, modelUUID := range modelUUIDs {
		objectDBs = append(objectDBs, ObjectDB{Namespace: modelUUID, DB: modelDBs[modelUUID]})
	}
	copied, err := LoadObjects(ctx, objectDBs, params.BundleDir, params.DataDir)
	if err != nil {
		return nil, errors.Capture(err)
	}
	params.Logger.Infof(ctx, "installed %d object store blobs", copied)

	// patchTarget is the archived machine the replacement maps onto:
	// the live controller machine resolved from the loaded dump, never
	// an assumed name.
	var patchTarget string
	if params.MachinePatch != nil {
		patch := *params.MachinePatch
		patchTarget, err = controllerMachineName(ctx, controllerModelDB)
		if err != nil {
			return nil, errors.Capture(err)
		}
		patch.MachineName = patchTarget
		if err := PatchControllerMachine(ctx, controllerModelDB, patch); err != nil {
			return nil, errors.Capture(err)
		}
	}

	if len(params.ControllerUnitAddresses) > 0 {
		// Kubernetes: the archived controller unit's addresses are dead
		// source-pod facts; patch them to the replacement's.
		if err := PatchControllerUnitAddresses(ctx, controllerModelDB, params.ControllerUnitAddresses); err != nil {
			return nil, errors.Capture(err)
		}
	}

	summary, err := buildSummary(ctx, info, controllerModelDB, modelDBs, patchTarget, copied)
	if err != nil {
		return nil, errors.Capture(err)
	}
	return summary, nil
}
