// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package recovery

import (
	"context"
	"os"
	"path/filepath"
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
	// LoadControllerDump validates native YAML before replacing controller
	// data in a single transaction.
	LoadControllerDump(context.Context, []byte) error

	// LoadModelDump validates native YAML before opening and replacing model
	// data. Imported placements are rebound to the replacement's object store
	// node supplied at construction.
	LoadModelDump(context.Context, coremodel.UUID, []byte) error

	// PatchControllerMachine resolves the live archived machine and applies
	// the replacement's physical facts, returning the chosen machine name.
	PatchControllerMachine(context.Context, coremodel.UUID, domainrecovery.MachinePatch) (string, error)

	// PatchControllerUnitAddresses replaces the controller unit's IP and DNS
	// observations: the unit on the named machine, or the lowest-named live
	// controller unit when the name is empty.
	PatchControllerUnitAddresses(context.Context, coremodel.UUID, string, []string) error

	PatchControllerCredential(context.Context, string, string, map[string]string) error
}

// CredentialPatch carries the replacement authentication for an archived
// credential: its identity is retained while the auth type and attributes
// are rebound.
type CredentialPatch struct {
	AuthType   string
	Attributes map[string]string
}

// LoadParams carries everything the database replay stage needs. It runs
// inside bootstrap, after Dqlite is initialised and before any agent, API or
// worker starts; the stage owns every database exclusively.
type LoadParams struct {
	// Service is fully constructed before recovery begins.
	Service RecoveryService

	// ControllerID is the provisioned replacement node's identity.
	ControllerID string

	// ControllerCredential rebinds the archived controller credential's
	// authentication to the credential used to provision the replacement.
	// Nil leaves the archived credential untouched.
	ControllerCredential *CredentialPatch

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
	if params.ControllerID == "" {
		return errors.New("missing controller ID")
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

	// patchTarget is the archived machine the replacement maps onto: the
	// live controller machine resolved from the loaded dump, never an
	// assumed name.
	var patchTarget string
	if params.MachinePatch != nil {
		patchTarget, err = params.Service.PatchControllerMachine(ctx,
			coremodel.UUID(params.ControllerModelUUID), *params.MachinePatch)
		if err != nil {
			return errors.Capture(err)
		}
	}

	if len(params.ControllerUnitAddresses) > 0 {
		// Synchronise the unit's IP and DNS observations with the
		// replacement before workers can publish them.
		if err := params.Service.PatchControllerUnitAddresses(ctx,
			coremodel.UUID(params.ControllerModelUUID), patchTarget,
			params.ControllerUnitAddresses); err != nil {
			return errors.Capture(err)
		}
	}

	if params.ControllerCredential != nil {
		patch := params.ControllerCredential
		if err := params.Service.PatchControllerCredential(ctx,
			params.ControllerModelUUID, patch.AuthType, patch.Attributes); err != nil {
			return errors.Capture(err)
		}
	}

	// The controller charm's install hook creates an empty controller.conf
	// that its Dqlite peer-relation hooks then maintain. A recovered unit
	// resumes from archived hook state, so install never runs; create the
	// file for the replacement's actual controller ID.
	charmConfPath := filepath.Join(params.DataDir,
		"agents", "controller-"+params.ControllerID, "controller.conf")
	if err := os.MkdirAll(filepath.Dir(charmConfPath), 0o700); err != nil {
		return errors.Errorf("creating controller charm config directory: %w", err)
	}
	if err := os.WriteFile(charmConfPath, nil, 0o600); err != nil {
		return errors.Errorf("creating controller charm config: %w", err)
	}
	return nil
}
