// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package recovery

import (
	"context"
	"database/sql"
	"os"
	"sort"

	"github.com/juju/juju/core/logger"
	domainrecovery "github.com/juju/juju/domain/recovery"
	recoverystate "github.com/juju/juju/domain/recovery/state"
	"github.com/juju/juju/internal/errors"
)

// LoadParams carries everything the recovery load stage needs. It runs
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
	// FQDNs). The unit's IP and DNS observations are replaced before the
	// api-address-setter can publish addresses from the archive.
	ControllerUnitAddresses []string

	// MachinePatch carries the replacement machine's physical facts.
	// Nil on Kubernetes, where there is no machine to patch.
	MachinePatch *domainrecovery.MachinePatch

	// ControllerCredential rebinds the archived controller credential's
	// authentication to the credential used to provision the replacement.
	// Nil leaves the archived credential untouched.
	ControllerCredential *CredentialPatch

	Logger logger.Logger
}

// CredentialPatch carries the replacement authentication for the archived
// controller credential: its identity is retained while the auth type and
// attributes are rebound.
type CredentialPatch struct {
	AuthType   string
	Attributes map[string]string
}

// Load executes the recovery stage: validate the uploaded archive, load
// the controller database, install the object blobs, load every model
// database and patch the replacement's physical facts in place. Any
// failure fails bootstrap; the target is discarded and bootstrap re-run.
func Load(ctx context.Context, params LoadParams) (*domainrecovery.Summary, error) {
	contents, info, err := ReadArchive(ctx, params.ArchivePath, params.ExpectedSHA256)
	if err != nil {
		return nil, errors.Capture(err)
	}
	defer func() { _ = contents.Close() }()
	params.Logger.Infof(ctx, "recovering controller %q from backup finished at %s",
		info.ControllerUUID, info.BackupFinished)

	controllerDump, err := domainrecovery.DecodeDump(contents.ControllerDump)
	if err != nil {
		return nil, errors.Capture(err)
	}
	if err := recoverystate.LoadControllerDump(ctx, params.ControllerDB, controllerDump); err != nil {
		return nil, errors.Capture(err)
	}

	modelUUIDs := make([]string, 0, len(contents.ModelDumps))
	for modelUUID := range contents.ModelDumps {
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
		data, err := os.ReadFile(contents.ModelDumps[modelUUID])
		if err != nil {
			return nil, errors.Errorf("reading dump for model %q: %w", modelUUID, err)
		}
		dump, err := domainrecovery.DecodeDump(data)
		if err != nil {
			return nil, errors.Errorf("model %q: %w", modelUUID, err)
		}
		if err := recoverystate.LoadModelDump(ctx, db, dump); err != nil {
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
		patchTarget = patch.MachineName
		if patchTarget == "" {
			patchTarget, err = recoverystate.ControllerMachineName(ctx, controllerModelDB)
		}
		if err != nil {
			return nil, errors.Capture(err)
		}
		patch.MachineName = patchTarget
		if err := recoverystate.PatchControllerMachine(ctx, controllerModelDB, patch); err != nil {
			return nil, errors.Capture(err)
		}
	}

	if len(params.ControllerUnitAddresses) > 0 {
		// Synchronise the unit's IP and DNS observations with the
		// replacement before workers can publish them.
		patch, err := domainrecovery.NewControllerUnitAddressPatch(params.ControllerUnitAddresses)
		if err != nil {
			return nil, errors.Capture(err)
		}
		if err := recoverystate.PatchControllerUnitAddresses(ctx, controllerModelDB, patch); err != nil {
			return nil, errors.Capture(err)
		}
	}

	if params.ControllerCredential != nil {
		patch := params.ControllerCredential
		if err := recoverystate.PatchControllerCredential(ctx, params.ControllerDB,
			params.ControllerModelUUID, patch.AuthType, patch.Attributes); err != nil {
			return nil, errors.Capture(err)
		}
	}

	summary, err := recoverystate.BuildSummary(ctx, info, controllerModelDB, modelDBs, patchTarget, copied)
	if err != nil {
		return nil, errors.Capture(err)
	}

	return summary, nil
}
