// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package controller

import (
	"context"

	"github.com/canonical/sqlair"

	"github.com/juju/juju/domain/export/types/controller/v4_1_0"
	"github.com/juju/juju/internal/errors"
)

// controllerTarget binds the current target controller UUID in the identity
// update WHERE clause.
type controllerTarget struct {
	UUID string `db:"uuid"`
}

// ControllerOverlay carries the target-local controller facts captured before
// import. The importer must not make hidden target reads: every target value
// it writes is passed here explicitly.
type ControllerOverlay struct {
	// ControllerID is the target controller node ID ("0"). It is used to
	// normalise object-store placement.
	ControllerID string

	// SourceController is the source controller identity row (UUID and
	// controller-model UUID) to restore. The controller table is a
	// singleton, so the target row is updated in place.
	SourceController v4_1_0.Controller

	// TargetControllerUUID is the temporary target controller UUID, matched
	// by the identity update.
	TargetControllerUUID string

	// RemoveNamespaces are target-only namespace_list rows to delete.
	RemoveNamespaces []v4_1_0.NamespaceList

	// AddNamespaces are source namespace_list rows to add.
	AddNamespaces []v4_1_0.NamespaceList
}

// modelUUID binds a controller model UUID in the temp-model deletion.
type modelUUID struct {
	UUID string `db:"uuid"`
}

// DeleteModel removes one model row (and its dependent rows) from the
// controller DB. It is run before the bulk import to eliminate the temporary
// bootstrap controller-model row: the source controller model carries the
// same qualified name (name, qualifier) and would otherwise collide with the
// temporary row on the unique index idx_model_qualified_name. The Deletion
// deletes the rows that foreign-key model(uuid) first so the immediate FK
// constraints are satisfied without deferrals.
//
// Note this is unlike [domain.SecretService.DeleteModel]: it does not touch
// any dqlite model database (the dbaccessor DBDeleter deletes the namespace
// database once the sequence has committed). Deleting the row here only
// removes the logical model record.
func (st *State) DeleteModel(ctx context.Context, uuid string) error {
	db, err := st.DB(ctx)
	if err != nil {
		return errors.Errorf("getting db: %w", err)
	}

	stmtSecretBackendReference, err := sqlair.Prepare(`DELETE FROM "secret_backend_reference" WHERE model_uuid = $modelUUID.uuid`, modelUUID{})
	if err != nil {
		return errors.Errorf("preparing secret_backend_reference delete statement: %w", err)
	}
	stmtModelSecretBackend, err := sqlair.Prepare(`DELETE FROM "model_secret_backend" WHERE model_uuid = $modelUUID.uuid`, modelUUID{})
	if err != nil {
		return errors.Errorf("preparing model_secret_backend delete statement: %w", err)
	}
	stmtModelLastLogin, err := sqlair.Prepare(`DELETE FROM "model_last_login" WHERE model_uuid = $modelUUID.uuid`, modelUUID{})
	if err != nil {
		return errors.Errorf("preparing model_last_login delete statement: %w", err)
	}
	stmtModelAuthorizedKeys, err := sqlair.Prepare(`DELETE FROM "model_authorized_keys" WHERE model_uuid = $modelUUID.uuid`, modelUUID{})
	if err != nil {
		return errors.Errorf("preparing model_authorized_keys delete statement: %w", err)
	}
	stmtModelNamespace, err := sqlair.Prepare(`DELETE FROM "model_namespace" WHERE model_uuid = $modelUUID.uuid`, modelUUID{})
	if err != nil {
		return errors.Errorf("preparing model_namespace delete statement: %w", err)
	}
	stmtModel, err := sqlair.Prepare(`DELETE FROM "model" WHERE uuid = $modelUUID.uuid`, modelUUID{})
	if err != nil {
		return errors.Errorf("preparing model delete statement: %w", err)
	}
	stmtObjectStorePlacement, err := sqlair.Prepare(`DELETE FROM "object_store_placement"`)
	if err != nil {
		return errors.Errorf("preparing object_store_placement delete statement: %w", err)
	}

	row := modelUUID{UUID: uuid}
	if err := db.Txn(ctx, func(ctx context.Context, tx *sqlair.TX) error {
		if err := tx.Query(ctx, stmtSecretBackendReference, row).Run(); err != nil {
			return errors.Errorf("deleting secret_backend_reference rows: %w", err)
		}
		if err := tx.Query(ctx, stmtModelSecretBackend, row).Run(); err != nil {
			return errors.Errorf("deleting model_secret_backend rows: %w", err)
		}
		if err := tx.Query(ctx, stmtModelLastLogin, row).Run(); err != nil {
			return errors.Errorf("deleting model_last_login rows: %w", err)
		}
		if err := tx.Query(ctx, stmtModelAuthorizedKeys, row).Run(); err != nil {
			return errors.Errorf("deleting model_authorized_keys rows: %w", err)
		}
		if err := tx.Query(ctx, stmtModelNamespace, row).Run(); err != nil {
			return errors.Errorf("deleting model_namespace rows: %w", err)
		}
		if err := tx.Query(ctx, stmtModel, row).Run(); err != nil {
			return errors.Errorf("deleting model row: %w", err)
		}
		// Placement references metadata replaced by the bulk import. The
		// overlay recreates source placement on this controller node.
		if err := tx.Query(ctx, stmtObjectStorePlacement).Run(); err != nil {
			return errors.Errorf("deleting object_store_placement rows: %w", err)
		}
		return nil
	}); err != nil {
		return errors.Errorf("deleting model %q: %w", uuid, err)
	}
	return nil
}

// ApplyOverlay writes the target-local controller facts captured before
// import onto the restored controller database. The source logical rows were
// imported by State.Import, which excludes the target-local tables
// (controller identity, node, config, changelog, namespace_list, object
// store). This phase:
//
//   - restores the source controller identity by updating the singleton
//     controller row in place (uuid and model_uuid become the source values;
//     api_port/cert/keys remain target-local);
//   - clears stale changelog and controller leases;
//   - makes namespace_list authoritative (only source namespaces remain);
//   - normalises object-store placement to node 0;
//   - removes restored AUTOINCREMENT entries from sqlite_sequence so SQLite
//     allocation starts after current maxima.
//
// The target controller_node/config/api-address facts are already correct
// (bootstrap-created and excluded from import) and are not rewritten.
func (st *State) ApplyOverlay(ctx context.Context, p *v4_1_0.ControllerExport, o ControllerOverlay) error {
	db, err := st.DB(ctx)
	if err != nil {
		return errors.Errorf("getting db: %w", err)
	}

	stmtUpdateController, err := sqlair.Prepare(`UPDATE "controller" SET uuid = $Controller.uuid, model_uuid = $Controller.model_uuid, target_version = $Controller.target_version, cert = $Controller.cert, ca_cert = $Controller.ca_cert, private_key = $Controller.private_key, ca_private_key = $Controller.ca_private_key, system_identity = $Controller.system_identity WHERE uuid = $controllerTarget.uuid`, v4_1_0.Controller{}, controllerTarget{})
	if err != nil {
		return errors.Errorf("preparing controller update statement: %w", err)
	}
	stmtClearChangeLog, err := sqlair.Prepare(`DELETE FROM "change_log"`)
	if err != nil {
		return errors.Errorf("preparing change_log clear statement: %w", err)
	}
	stmtClearChangeLogWitness, err := sqlair.Prepare(`DELETE FROM "change_log_witness"`)
	if err != nil {
		return errors.Errorf("preparing change_log_witness clear statement: %w", err)
	}
	// Lease pins are cleared before and after the lease delete: lease_pin
	// has an immediate (non-deferred) FK to lease, so the pins attached to
	// type-0 leases must go first; any pins still dangling afterwards (e.g.
	// restored without their lease) are then swept.
	stmtClearLeasedPins, err := sqlair.Prepare(`DELETE FROM "lease_pin" WHERE lease_uuid IN (SELECT uuid FROM "lease" WHERE lease_type_id = 0)`)
	if err != nil {
		return errors.Errorf("preparing lease_pin clear statement: %w", err)
	}
	stmtClearLeases, err := sqlair.Prepare(`DELETE FROM "lease" WHERE lease_type_id = 0`)
	if err != nil {
		return errors.Errorf("preparing lease clear statement: %w", err)
	}
	stmtClearOrphanPins, err := sqlair.Prepare(`DELETE FROM "lease_pin" WHERE lease_uuid NOT IN (SELECT uuid FROM "lease")`)
	if err != nil {
		return errors.Errorf("preparing orphan lease_pin clear statement: %w", err)
	}
	stmtDeleteNamespace, err := sqlair.Prepare(`DELETE FROM "namespace_list" WHERE namespace = $NamespaceList.namespace`, v4_1_0.NamespaceList{})
	if err != nil {
		return errors.Errorf("preparing namespace_list delete statement: %w", err)
	}
	stmtInsertNamespace, err := sqlair.Prepare(`INSERT INTO "namespace_list" (namespace) VALUES ($NamespaceList.namespace) ON CONFLICT DO NOTHING`, v4_1_0.NamespaceList{})
	if err != nil {
		return errors.Errorf("preparing namespace_list insert statement: %w", err)
	}
	stmtClearPlacement, err := sqlair.Prepare(`DELETE FROM "object_store_placement"`)
	if err != nil {
		return errors.Errorf("preparing object_store_placement clear statement: %w", err)
	}
	stmtInsertPlacement, err := sqlair.Prepare(`INSERT INTO "object_store_placement" (uuid, node_id) VALUES ($ObjectStorePlacement.uuid, $ObjectStorePlacement.node_id)`, v4_1_0.ObjectStorePlacement{})
	if err != nil {
		return errors.Errorf("preparing object_store_placement insert statement: %w", err)
	}
	stmtClearSequence, err := sqlair.Prepare(`DELETE FROM "sqlite_sequence"`)
	if err != nil {
		return errors.Errorf("preparing sqlite_sequence clear statement: %w", err)
	}

	if err := db.Txn(ctx, func(ctx context.Context, tx *sqlair.TX) error {
		// Restore the source controller identity on the singleton row.
		if err := tx.Query(ctx, stmtUpdateController, o.SourceController, controllerTarget{UUID: o.TargetControllerUUID}).Run(); err != nil {
			return errors.Errorf("updating controller identity: %w", err)
		}

		// Clear stale changelog and controller leases.
		if err := tx.Query(ctx, stmtClearChangeLog).Run(); err != nil {
			return errors.Errorf("clearing change_log: %w", err)
		}
		if err := tx.Query(ctx, stmtClearChangeLogWitness).Run(); err != nil {
			return errors.Errorf("clearing change_log_witness: %w", err)
		}
		if err := tx.Query(ctx, stmtClearLeasedPins).Run(); err != nil {
			return errors.Errorf("clearing pins of controller leases: %w", err)
		}
		if err := tx.Query(ctx, stmtClearLeases).Run(); err != nil {
			return errors.Errorf("clearing controller leases: %w", err)
		}
		if err := tx.Query(ctx, stmtClearOrphanPins).Run(); err != nil {
			return errors.Errorf("clearing orphaned lease pins: %w", err)
		}

		// Make namespace_list authoritative: only source namespaces remain.
		for _, ns := range o.RemoveNamespaces {
			if err := tx.Query(ctx, stmtDeleteNamespace, ns).Run(); err != nil {
				return errors.Errorf("removing namespace %q: %w", ns.Namespace, err)
			}
		}
		for _, ns := range o.AddNamespaces {
			if err := tx.Query(ctx, stmtInsertNamespace, ns).Run(); err != nil {
				return errors.Errorf("adding namespace %q: %w", ns.Namespace, err)
			}
		}

		// Normalise object-store placement to node 0.
		if err := tx.Query(ctx, stmtClearPlacement).Run(); err != nil {
			return errors.Errorf("clearing object_store_placement: %w", err)
		}
		for _, pl := range p.ObjectStorePlacement {
			pl.NodeID = o.ControllerID
			if err := tx.Query(ctx, stmtInsertPlacement, pl).Run(); err != nil {
				return errors.Errorf("inserting object_store_placement: %w", err)
			}
		}

		// Remove restored AUTOINCREMENT entries so SQLite allocates after
		// current maxima.
		if err := tx.Query(ctx, stmtClearSequence).Run(); err != nil {
			return errors.Errorf("clearing sqlite_sequence: %w", err)
		}

		return nil
	}); err != nil {
		return errors.Errorf("applying controller overlay: %w", err)
	}

	return nil
}
