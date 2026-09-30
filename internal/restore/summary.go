// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package restore

import (
	"context"
	"database/sql"
	"time"

	"github.com/juju/juju/internal/errors"
)

// Summary is the restore report printed when bootstrap finishes. It tells
// the operator what was restored and what needs attention, because the
// controller reconciles the moment it starts: there is no approval gate.
type Summary struct {
	// ControllerUUID and ControllerName are the restored identities.
	ControllerUUID string
	ControllerName string

	// SourceAgentVersion is the agent version the archive was taken from
	// (equal to the running version; the gate enforced it).
	SourceAgentVersion string

	// BackupFinished is when the source backup completed; its age tells
	// the operator how much state drift to expect.
	BackupFinished time.Time

	// HANodes is the number of controller nodes the source had.
	HANodes int64

	// DeadControllerMachines lists the source's other controller machine
	// names (HA sources only). They load as dead machines; the operator
	// removes them after restore.
	DeadControllerMachines []string

	// Models is the number of restored models, including the controller
	// model.
	Models int

	// ObjectsCopied is the number of object-store blobs installed into
	// the replacement's file-backed store.
	ObjectsCopied int

	// MachinesNotAlive, ApplicationsNotAlive and UnitsNotAlive count
	// entities whose restored life is not alive, aggregated over all
	// model databases. Their pending removals run as soon as
	// reconciliation starts.
	MachinesNotAlive     int
	ApplicationsNotAlive int
	UnitsNotAlive        int

	// PendingRemovals counts rows in the model removal tables: scheduled
	// and forced removals that execute once the controller starts.
	PendingRemovals int
}

// lifeAlive is the life.id value for alive entities, fixed by the schema
// seed data under the exact-version gate.
const lifeAlive = 0

// buildSummary collects the report from the loaded databases. modelDBs
// holds one open database per restored model, keyed by model UUID; the
// controller model's database is included.
func buildSummary(ctx context.Context, info *ArchiveInfo, controllerModelDB *sql.DB, modelDBs map[string]*sql.DB, machineName string, objectsCopied int) (*Summary, error) {
	summary := &Summary{
		ControllerUUID:     info.ControllerUUID,
		ControllerName:     info.ControllerName,
		SourceAgentVersion: info.AgentVersion.String(),
		HANodes:            info.HANodes,
		Models:             len(info.Models),
		ObjectsCopied:      objectsCopied,
	}

	// The dead controller machines are the archived controller
	// application's machines other than the replacement, regardless of
	// the manifest's HA node count: the manifest may understate the HA
	// topology, and machines hosting units of the controller application
	// are controller machines even when they also host workload units.
	dead, err := otherControllerMachines(ctx, controllerModelDB, machineName)
	if err != nil {
		return nil, errors.Capture(err)
	}
	summary.DeadControllerMachines = dead

	for _, db := range modelDBs {
		machines, err := countNotAlive(ctx, db, "machine")
		if err != nil {
			return nil, errors.Capture(err)
		}
		apps, err := countNotAlive(ctx, db, "application")
		if err != nil {
			return nil, errors.Capture(err)
		}
		units, err := countNotAlive(ctx, db, "unit")
		if err != nil {
			return nil, errors.Capture(err)
		}
		removals, err := countRows(ctx, db, "removal")
		if err != nil {
			return nil, errors.Capture(err)
		}
		summary.MachinesNotAlive += machines
		summary.ApplicationsNotAlive += apps
		summary.UnitsNotAlive += units
		summary.PendingRemovals += removals
	}
	return summary, nil
}

// otherControllerMachines lists the machines hosting units of the
// controller application, other than the replacement's machine. They are
// the source's other controller machines, awaiting operator removal after
// restore.
func otherControllerMachines(ctx context.Context, db *sql.DB, machineName string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `
SELECT m.name FROM machine m
JOIN v_machine_is_controller vic ON vic.machine_uuid = m.uuid
WHERE m.name != ?
ORDER BY m.name`, machineName)
	if err != nil {
		return nil, errors.Errorf("listing dead controller machines: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, errors.Capture(err)
		}
		names = append(names, name)
	}
	return names, errors.Capture(rows.Err())
}

// countNotAlive counts entities whose life is not alive.
func countNotAlive(ctx context.Context, db *sql.DB, table string) (int, error) {
	var count int
	err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM "+table+" WHERE life_id != ?", lifeAlive).Scan(&count)
	if err != nil {
		return 0, errors.Errorf("counting %s rows: %w", table, err)
	}
	return count, nil
}

// countRows counts every row in table.
func countRows(ctx context.Context, db *sql.DB, table string) (int, error) {
	var count int
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count)
	if err != nil {
		return 0, errors.Errorf("counting %s rows: %w", table, err)
	}
	return count, nil
}
