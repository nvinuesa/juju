// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package restore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"

	"github.com/canonical/sqlair"

	"github.com/juju/juju/core/database"
	"github.com/juju/juju/core/logger"
	ctrlv4_1_0 "github.com/juju/juju/domain/export/types/controller/v4_1_0"
	"github.com/juju/juju/domain/restoreimport"
	"github.com/juju/juju/internal/errors"
)

// DBGetter is the reduced-runtime Dqlite access the restore needs. It is the
// same interface the db-accessor worker exports in safe mode.
type DBGetter interface {
	GetDB(ctx context.Context, namespace string) (database.TxnRunner, error)
}

// DBDeleter deletes a model database namespace.
type DBDeleter interface {
	DeleteDB(namespace string) error
}

// Restorer orchestrates the restore phases in the order the spec requires.
// Archive preflight and blob staging happen before any database mutation;
// imports and overlays run last; the object store is swapped only after all
// database writes and configuration regeneration succeed.
type Restorer struct {
	// DBGetter opens controller/model databases (reduced runtime).
	DBGetter DBGetter

	// DBDeleter deletes the temporary target controller-model database.
	DBDeleter DBDeleter

	// StageDir is the private staging tree for archive blobs.
	StageDir string

	// ObjectStoreRoot is the target file-object-store root directory.
	ObjectStoreRoot string

	// DataDir is the target Juju data directory (the parent of
	// ObjectStoreRoot); the machine-0 agent config is regenerated here.
	DataDir string

	// Logger receives phase logs.
	Logger logger.Logger

	// Progress receives user-facing phase updates when set.
	Progress func(string)
}

// Restore runs the full restore pipeline for the archive at path.
func (r *Restorer) Restore(ctx context.Context, path string) error {
	opener := OpenArchiveFile(path)

	r.reportProgress("restore: validating archive")
	archive, err := Preflight(ctx, opener)
	if err != nil {
		return err
	}
	r.reportProgress("restore: archive preflight complete")
	r.Logger.Infof(ctx, "restore: archive preflight complete")

	r.reportProgress("restore: staging object store")
	if err := Stage(ctx, opener, archive, r.StageDir); err != nil {
		return err
	}
	r.reportProgress("restore: object store staged")
	r.Logger.Infof(ctx, "restore: object store staged")

	r.reportProgress("restore: importing databases")
	if err := r.importDatabases(ctx, archive); err != nil {
		return err
	}
	r.reportProgress("restore: databases restored")
	r.Logger.Infof(ctx, "restore: databases restored")

	r.reportProgress("restore: swapping object store")
	if err := r.swapObjectStore(ctx); err != nil {
		return err
	}
	r.reportProgress("restore: object store swapped")
	r.Logger.Infof(ctx, "restore: object store swapped")

	r.reportProgress("restore: regenerating agent configuration")
	if err := r.regenerateAgentConfig(archive); err != nil {
		return err
	}
	r.reportProgress("restore: agent configuration regenerated")
	r.Logger.Infof(ctx, "restore: agent configuration regenerated")

	return nil
}

func (r *Restorer) reportProgress(message string) {
	if r.Progress != nil {
		r.Progress(message)
	}
}

// importDatabases runs the namespace/model/controller import sequence:
//
//  1. capture the target-local overlay;
//  2. register source model namespace IDs in namespace_list (the temporary
//     target controller-model namespace is retained until deletion);
//  3. import every source model database schema-first;
//  4. import controller logical rows last and make namespace_list
//     authoritative;
//  5. delete the temporary target controller-model database.
//
// DBGetter rejects unregistered namespaces, hence registration precedes
// model import. DBDeleter cannot delete the controller DB, hence the
// controller is imported in place.
func (r *Restorer) importDatabases(ctx context.Context, archive *Archive) error {
	ctrlDB, err := r.DBGetter.GetDB(ctx, database.ControllerNS)
	if err != nil {
		return errors.Errorf("opening controller DB: %w", err)
	}

	target, err := CaptureTarget(ctx, ctrlDB, r.DBGetter)
	if err != nil {
		return err
	}

	// The target must be a fresh/disposable controller structurally matching
	// the source's cloud identity. No mutation happens in this check.
	if err := checkTargetCompatibility(archive, target); err != nil {
		return err
	}

	// Register source model namespaces before opening any model DB.
	if err := registerNamespaces(ctx, ctrlDB, archive, target); err != nil {
		return err
	}

	// Import each source model database.
	for modelUUID, payload := range archive.Models {
		modelFactory := func(ctx context.Context) (database.TxnRunner, error) {
			return r.DBGetter.GetDB(ctx, modelUUID)
		}
		importer := restoreimport.NewModelImporter(modelFactory)
		overlay := restoreimport.ModelOverlay{NodeID: target.ControllerID}
		if modelUUID == archive.ControllerModelUUID {
			overlay.ModelAgentPasswordHash = target.ModelAgentPasswordHash
			overlay.Machine0PasswordHash = target.Machine0PasswordHash
			overlay.Machine0AlgorithmID = target.Machine0AlgorithmID
			overlay.Machine0 = target.Machine0
			overlay.Machine0CloudInstance = target.Machine0CloudInstance
			overlay.ControllerUnitNetNodeUUID = target.ControllerUnitNetNodeUUID
			overlay.NetNodes = target.NetNodes
			overlay.LinkLayerDevices = target.LinkLayerDevices
			overlay.IPAddresses = target.IPAddresses
		}
		if err := importer.Import(ctx, payload, overlay); err != nil {
			return errors.Errorf("importing model %q: %w: %v", modelUUID, ErrMutation, err)
		}
	}

	// Import controller logical rows last, overlaying target-local facts.
	ctrlFactory := func(ctx context.Context) (database.TxnRunner, error) {
		return r.DBGetter.GetDB(ctx, database.ControllerNS)
	}
	ctrlImporter := restoreimport.NewControllerImporter(ctrlFactory)
	overlay := controllerOverlayFor(target, archive)
	if err := ctrlImporter.Import(ctx, archive.Controller, overlay, target.ControllerModelUUID); err != nil {
		return errors.Errorf("importing controller: %w: %v", ErrMutation, err)
	}

	// Delete the temporary target controller-model database.
	if target.ControllerModelUUID != archive.ControllerModelUUID {
		if err := r.DBDeleter.DeleteDB(target.ControllerModelUUID); err != nil {
			return errors.Errorf("deleting temporary controller-model DB: %w: %v", ErrMutation, err)
		}
	}

	return nil
}

// controllerOverlayFor builds the controller overlay from the captured target
// and the source namespaces.
func controllerOverlayFor(target *Target, archive *Archive) restoreimport.ControllerOverlay {
	// Source namespaces are the model UUIDs in the archive.
	sourceNamespaces := make(map[string]bool, len(archive.Models))
	for uuid := range archive.Models {
		sourceNamespaces[uuid] = true
	}

	overlay := restoreimport.ControllerOverlay{
		ControllerID:         target.ControllerID,
		TargetControllerUUID: target.ControllerUUID,
		SourceController:     archive.Controller.Controller[0],
	}
	for _, ns := range target.ModelNamespaces {
		// The controller's own namespace is not a model namespace: it must
		// survive, or the restored controller loses its database registry.
		if ns == database.ControllerNS {
			continue
		}
		if !sourceNamespaces[ns] {
			overlay.RemoveNamespaces = append(overlay.RemoveNamespaces, ctrlv4_1_0.NamespaceList{Namespace: ns})
		}
	}
	for uuid := range archive.Models {
		overlay.AddNamespaces = append(overlay.AddNamespaces, ctrlv4_1_0.NamespaceList{Namespace: uuid})
	}
	return overlay
}

// registerNamespaces inserts every source model namespace into the target
// controller namespace_list. The temporary target controller-model namespace
// is retained until deletion.
func registerNamespaces(ctx context.Context, ctrlDB database.TxnRunner, archive *Archive, target *Target) error {
	stmt, err := sqlair.Prepare(`INSERT INTO "namespace_list" (namespace) VALUES ($NamespaceList.namespace) ON CONFLICT DO NOTHING`, ctrlv4_1_0.NamespaceList{})
	if err != nil {
		return errors.Errorf("preparing namespace_list insert statement: %w", err)
	}
	registered := make(map[string]bool, len(target.ModelNamespaces))
	for _, ns := range target.ModelNamespaces {
		registered[ns] = true
	}
	for uuid := range archive.Models {
		if registered[uuid] {
			continue
		}
		if err := ctrlDB.Txn(ctx, func(ctx context.Context, tx *sqlair.TX) error {
			return errors.Capture(tx.Query(ctx, stmt, ctrlv4_1_0.NamespaceList{Namespace: uuid}).Run())
		}); err != nil {
			return errors.Errorf("registering model namespace %q: %w", uuid, err)
		}
	}
	return nil
}

// checkTargetCompatibility enforces the spec's target support gate before
// any database mutation: the target must have exactly one temporary controller
// model and structurally match the source's cloud type, cloud/region, and
// credential identity. These are structural checks only; no provider API calls
// are made.
func checkTargetCompatibility(archive *Archive, target *Target) error {
	// Freshness: exactly one temporary controller model.
	if target.ModelCount != 1 {
		return errors.Errorf("target controller has %d models, restore requires a fresh or disposable target with only a controller model: %w",
			target.ModelCount, ErrTargetIncompatible)
	}

	srcName, srcType, srcRegions, srcCredName, srcCredType := sourceCloudIdentity(archive)
	if target.CloudName != srcName {
		return errors.Errorf("target cloud %q does not match source cloud %q: %w",
			target.CloudName, srcName, ErrTargetIncompatible)
	}
	if target.CloudType != srcType {
		return errors.Errorf("target cloud type %q does not match source cloud type %q: %w",
			target.CloudType, srcType, ErrTargetIncompatible)
	}
	sort.Strings(srcRegions)
	sort.Strings(target.Regions)
	if !slices.Equal(srcRegions, target.Regions) {
		return errors.Errorf("target cloud regions %v do not match source cloud regions %v: %w",
			target.Regions, srcRegions, ErrTargetIncompatible)
	}
	if target.CredentialName != srcCredName {
		return errors.Errorf("target credential %q does not match source credential %q: %w",
			target.CredentialName, srcCredName, ErrTargetIncompatible)
	}
	if srcCredType != "" && target.CredentialAuthType != srcCredType {
		return errors.Errorf("target credential auth type %q does not match source credential auth type %q: %w",
			target.CredentialAuthType, srcCredType, ErrTargetIncompatible)
	}
	return nil
}

// sourceCloudIdentity extracts the source's single cloud identity from the
// decoded controller payload: name, type text, region names, and the
// credential identity that the controller model references (name and auth
// type text).
func sourceCloudIdentity(archive *Archive) (name, cloudType string, regions []string, credName, credType string) {
	c := archive.Controller
	cloud := c.Cloud[0]
	name = cloud.Name
	for _, ct := range c.CloudType {
		if ct.ID != nil && *ct.ID == cloud.CloudTypeID {
			cloudType = ct.Type
			break
		}
	}
	for _, r := range c.CloudRegion {
		if r.CloudUUID == cloud.UUID {
			regions = append(regions, r.Name)
		}
	}

	// The controller model's credential.
	var credUUID string
	for _, m := range c.Model {
		if m.UUID == archive.ControllerModelUUID {
			if m.CloudCredentialUUID != nil {
				credUUID = *m.CloudCredentialUUID
			}
			break
		}
	}
	if credUUID == "" {
		return name, cloudType, regions, "", ""
	}
	for _, cc := range c.CloudCredential {
		if cc.UUID == credUUID {
			credName = cc.Name
			for _, at := range c.AuthType {
				if at.Type != nil && fmt.Sprintf("%d", *at.ID) == cc.AuthTypeID {
					credType = *at.Type
					break
				}
			}
			break
		}
	}
	return name, cloudType, regions, credName, credType
}

// swapObjectStore swaps the staged blob tree into the target object-store
// root. Renames within one filesystem make the swap near-atomic: the
// existing target tree is first moved aside, the staged tree moved into
// place, and only then is the old tree discarded. If the final move fails the
// previous tree is moved back.
//
// StageDir must live on the same filesystem as ObjectStoreRoot (the launcher
// places it beside the root), otherwise the rename fails with EXDEV. The
// restore never deletes the previous object store before its replacement is
// fully in place.
func (r *Restorer) swapObjectStore(ctx context.Context) error {
	staged := filepath.Join(r.StageDir, objectStoreDir)
	if _, err := os.Stat(staged); err != nil {
		return errors.Errorf("staged object store missing: %w: %v", ErrFinalization, err)
	}
	target := r.ObjectStoreRoot
	previous := target + ".restore-previous"

	// Move an existing target tree aside; it is retained until the staged
	// tree is in place.
	hadPrevious := false
	if _, err := os.Stat(target); err == nil {
		if err := os.Rename(target, previous); err != nil {
			return errors.Errorf("moving previous object store aside: %w: %v", ErrFinalization, err)
		}
		hadPrevious = true
	}

	if err := os.Rename(staged, target); err != nil {
		if hadPrevious {
			// Reinstate the previous tree before reporting failure.
			_ = os.Rename(previous, target)
		}
		return errors.Errorf("swapping object store: %w: %v", ErrFinalization, err)
	}

	// Swap completed: drop the retained previous tree.
	if hadPrevious {
		if err := os.RemoveAll(previous); err != nil {
			r.Logger.Warningf(ctx, "restore: dropping previous object store %q: %v", previous, err)
		}
	}
	return nil
}
