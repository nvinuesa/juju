// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package state

import (
	"context"
	"database/sql"

	domainrecovery "github.com/juju/juju/domain/recovery"
	"github.com/juju/juju/internal/errors"
)

// lifeAlive is the life.id value for alive entities, fixed by the schema
// seed data under the exact-version gate.
const lifeAlive = 0

// BuildSummary collects the report from the loaded databases. modelDBs
// holds one open database per recovered model, keyed by model UUID; the
// controller model's database is included.
func BuildSummary(ctx context.Context, info *domainrecovery.ArchiveInfo, controllerModelDB *sql.DB, modelDBs map[string]*sql.DB, machineName string, objectsCopied int) (*domainrecovery.Summary, error) {
	summary := &domainrecovery.Summary{
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
// recovery.
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
