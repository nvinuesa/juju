// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package restore

import (
	"context"
	"database/sql"
	"strings"

	"github.com/juju/juju/internal/errors"
	"github.com/juju/juju/internal/uuid"
)

// MachinePatch carries the replacement machine's physical facts, known to
// bootstrap because it just created them.
type MachinePatch struct {
	// MachineName is the name of the archived controller machine the
	// replacement maps onto. It is resolved from the loaded dump by the
	// restore stage; the caller need not set it.
	MachineName string

	// InstanceID is the replacement's actual provider instance ID.
	InstanceID string

	// DisplayName is the replacement's provider display name.
	DisplayName string

	// Arch is the replacement's architecture.
	Arch string

	// MemMB, Cores and RootDiskMB are the replacement's observed
	// hardware. Zero values leave the archived observations in place.
	MemMB      uint64
	Cores      uint64
	RootDiskMB uint64
}

// controllerMachineName resolves the archived machine the replacement
// maps onto: the lowest-named ALIVE machine hosting a unit of the
// controller application. An HA source may have lost its "0" machine; the
// patch target must be a machine that actually exists and is alive in the
// archive, never an assumed ordinal.
func controllerMachineName(ctx context.Context, modelDB *sql.DB) (string, error) {
	var name string
	err := modelDB.QueryRowContext(ctx, `
SELECT m.name FROM machine m
JOIN v_machine_is_controller vic ON vic.machine_uuid = m.uuid
WHERE m.life_id = 0
ORDER BY m.name
LIMIT 1`).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errors.Errorf("archive contains no live controller machine")
	} else if err != nil {
		return "", errors.Errorf("finding archived controller machine: %w", err)
	}
	return name, nil
}

// PatchControllerMachine writes the replacement's physical facts over the
// archived controller machine's cloud instance record. Logical identities
// — machine UUID and name — are never rewritten. Network and storage
// observations are left as archived: the instance poller refreshes them
// after the first start.
func PatchControllerMachine(ctx context.Context, modelDB *sql.DB, patch MachinePatch) error {
	var machineUUID string
	err := modelDB.QueryRowContext(ctx,
		"SELECT uuid FROM machine WHERE name = ?", patch.MachineName).Scan(&machineUUID)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.Errorf("archived controller machine %q not found", patch.MachineName)
	} else if err != nil {
		return errors.Errorf("finding archived controller machine %q: %w", patch.MachineName, err)
	}

	sets := []string{"instance_id = ?", "display_name = ?"}
	args := []any{patch.InstanceID, patch.DisplayName}
	if patch.Arch != "" {
		sets = append(sets, "arch = ?")
		args = append(args, patch.Arch)
	}
	if patch.MemMB > 0 {
		sets = append(sets, "mem = ?")
		args = append(args, patch.MemMB)
	}
	if patch.Cores > 0 {
		sets = append(sets, "cpu_cores = ?")
		args = append(args, patch.Cores)
	}
	if patch.RootDiskMB > 0 {
		sets = append(sets, "root_disk = ?")
		args = append(args, patch.RootDiskMB)
	}
	args = append(args, machineUUID)

	res, err := modelDB.ExecContext(ctx,
		"UPDATE machine_cloud_instance SET "+strings.Join(sets, ", ")+" WHERE machine_uuid = ?",
		args...)
	if err != nil {
		return errors.Errorf("patching controller machine %q: %w", patch.MachineName, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return errors.Capture(err)
	}
	if affected == 0 {
		return errors.Errorf(
			"archived controller machine %q has no cloud instance record", patch.MachineName)
	}
	return nil
}

// PatchControllerUnitAddresses rewrites the controller unit's address
// observations to the replacement's addresses (on Kubernetes, the stable
// per-ordinal controller pod FQDN). The archived rows hold the source pod's
// IP, which is dead on the replacement, and the api-address-setter
// republishes the unit's addresses as the controller's API address to every
// agent. Existing rows are rewritten in order, stale surplus rows are
// removed, and surplus replacement addresses are inserted by cloning an
// archived row's device and classification references. If the archive
// records no address rows for the controller unit, the replacement's
// addresses cannot be patched and the load fails.
func PatchControllerUnitAddresses(ctx context.Context, db *sql.DB, addresses []string) error {
	if len(addresses) == 0 {
		return nil
	}

	var netNodeUUID string
	err := db.QueryRowContext(ctx,
		"SELECT net_node_uuid FROM unit WHERE name = 'controller/0'",
	).Scan(&netNodeUUID)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.Errorf("controller unit %q not found", "controller/0")
	} else if err != nil {
		return errors.Errorf("finding controller unit: %w", err)
	}

	rows, err := db.QueryContext(ctx,
		"SELECT uuid FROM ip_address WHERE net_node_uuid = ? ORDER BY uuid", netNodeUUID)
	if err != nil {
		return errors.Errorf("reading controller unit addresses: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var rowUUIDs []string
	for rows.Next() {
		var uuid string
		if err := rows.Scan(&uuid); err != nil {
			return errors.Errorf("reading controller unit address: %w", err)
		}
		rowUUIDs = append(rowUUIDs, uuid)
	}
	if err := rows.Err(); err != nil {
		return errors.Capture(err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return errors.Capture(err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	for i, addr := range addresses {
		if i >= len(rowUUIDs) {
			break
		}
		if _, err := tx.ExecContext(ctx,
			"UPDATE ip_address SET address_value = ? WHERE uuid = ?",
			addr, rowUUIDs[i],
		); err != nil {
			return errors.Errorf("patching controller unit address: %w", err)
		}
	}
	for _, uuid := range rowUUIDs[min(len(addresses), len(rowUUIDs)):] {
		// Remove the stale surplus: every remaining row still holds the
		// source pod's address.
		if _, err := tx.ExecContext(ctx,
			"DELETE FROM ip_address WHERE uuid = ?", uuid,
		); err != nil {
			return errors.Errorf("removing stale controller unit address: %w", err)
		}
	}

	if len(addresses) > len(rowUUIDs) {
		// The replacement exposes more addresses than the archive
		// recorded: insert the surplus, cloning an archived row's
		// device and classification so the new rows satisfy the same
		// references.
		if len(rowUUIDs) == 0 {
			return errors.Errorf(
				"archive records no ip_address rows for the controller unit; cannot patch %d addresses",
				len(addresses))
		}
		var deviceUUID string
		var subnetUUID any
		var typeID, configTypeID, originID, scopeID int64
		err := tx.QueryRowContext(ctx, `
SELECT device_uuid, subnet_uuid, type_id, config_type_id, origin_id, scope_id
FROM ip_address WHERE uuid = ?`, rowUUIDs[0],
		).Scan(&deviceUUID, &subnetUUID, &typeID, &configTypeID, &originID, &scopeID)
		if err != nil {
			return errors.Errorf("reading controller unit address template: %w", err)
		}
		for _, addr := range addresses[len(rowUUIDs):] {
			newUUID, err := uuid.NewUUID()
			if err != nil {
				return errors.Errorf("generating controller unit address uuid: %w", err)
			}
			if _, err := tx.ExecContext(ctx, `
INSERT INTO ip_address
	(uuid, net_node_uuid, device_uuid, address_value, subnet_uuid,
	type_id, config_type_id, origin_id, scope_id)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				newUUID.String(), netNodeUUID, deviceUUID, addr, subnetUUID,
				typeID, configTypeID, originID, scopeID,
			); err != nil {
				return errors.Errorf("inserting controller unit address: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return errors.Errorf("committing controller unit address patch: %w", err)
	}
	committed = true
	return nil
}
