// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package recovery

import (
	"context"
	"os"
	"sort"

	"github.com/juju/juju/core/logger"
	coremodel "github.com/juju/juju/core/model"
	"github.com/juju/juju/internal/errors"
)

// RecoveryService provides persistence behind the domain service boundary.
// It operates within an exclusive bootstrap database session. Loads commit
// independently; a later failure requires discarding the target.
type RecoveryService interface {
	// LoadControllerDump validates native YAML before replacing controller
	// data in a single transaction.
	LoadControllerDump(context.Context, []byte) error

	// LoadModelDump validates native YAML before opening and replacing model
	// data. Imported placements are rebound to the replacement's object store
	// node supplied at construction.
	LoadModelDump(context.Context, coremodel.UUID, []byte) error
}

// LoadParams carries everything the database replay stage needs. It runs
// inside bootstrap, after Dqlite is initialised and before any agent, API or
// worker starts; the stage owns every database exclusively.
type LoadParams struct {
	// Service is fully constructed before recovery begins.
	Service RecoveryService

	// ArchivePath is the uploaded archive on the target's disk.
	ArchivePath string

	// ExpectedSHA256 is the operator-supplied archive checksum (hex). It is
	// required: the archive is trusted only after it matches.
	ExpectedSHA256 string

	// DataDir is the replacement's data directory. Staged dumps live here
	// so they share the filesystem that holds the imported databases.
	DataDir string

	// ControllerModelUUID identifies the controller model the archive must
	// contain.
	ControllerModelUUID string

	Logger logger.Logger
}

// Load replays the archived controller and model database dumps into the
// replacement's Dqlite databases. Physical state is not reconciled and the
// object store is not unpacked here.
func Load(ctx context.Context, params LoadParams) (err error) {
	if params.Service == nil {
		return errors.New("missing recovery service")
	}
	if params.Logger == nil {
		return errors.New("missing logger")
	}
	if params.ArchivePath == "" {
		return errors.New("missing archive path")
	}
	if params.DataDir == "" {
		return errors.New("missing data directory")
	}
	if params.ExpectedSHA256 == "" {
		return errors.New("missing expected archive checksum")
	}
	if params.ControllerModelUUID == "" {
		return errors.New("missing controller model UUID")
	}

	contents, info, err := ReadArchiveIn(
		ctx, params.Logger, params.ArchivePath, params.ExpectedSHA256, params.DataDir,
	)
	if err != nil {
		return errors.Capture(err)
	}
	defer func() {
		if closeErr := contents.Close(); closeErr != nil {
			err = errors.Join(err, errors.Errorf("cleaning up staged recovery dumps: %w", closeErr))
		}
	}()
	if info.ControllerModelUUID != params.ControllerModelUUID {
		return errors.Errorf(
			"archive controller model %q does not match the bootstrap controller model %q",
			info.ControllerModelUUID, params.ControllerModelUUID)
	}

	if err := params.Service.LoadControllerDump(ctx, contents.ControllerDump); err != nil {
		return errors.Capture(err)
	}

	modelUUIDs := make([]string, 0, len(contents.ModelDumps))
	for modelUUID := range contents.ModelDumps {
		modelUUIDs = append(modelUUIDs, modelUUID)
	}
	sort.Strings(modelUUIDs)

	for _, modelUUID := range modelUUIDs {
		if err := ctx.Err(); err != nil {
			return errors.Capture(err)
		}
		data, err := os.ReadFile(contents.ModelDumps[modelUUID])
		if err != nil {
			return errors.Errorf("reading dump for model %q: %w", modelUUID, err)
		}
		if err := params.Service.LoadModelDump(ctx, coremodel.UUID(modelUUID), data); err != nil {
			return errors.Capture(err)
		}
		params.Logger.Tracef(ctx, "loaded database for model %s", modelUUID)
	}
	return nil
}
