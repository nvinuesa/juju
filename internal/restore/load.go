// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package restore

import (
	"context"
	"database/sql"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/juju/juju/core/semversion"
	domainexport "github.com/juju/juju/domain/export"
	"github.com/juju/juju/internal/errors"
)

// LoadControllerDump loads the archived controller database dump into the
// freshly bootstrapped controller database. The load runs inside one
// transaction before any agent, API or worker starts; a failure rolls
// back, failing bootstrap.
func LoadControllerDump(ctx context.Context, db *sql.DB, dump *Dump) error {
	if err := checkDumpVersion(dump, domainexport.ControllerExportVersions); err != nil {
		return errors.Errorf("controller dump: %w", err)
	}
	if err := loadDatabase(ctx, db, dump, controllerTablePolicies, true); err != nil {
		return errors.Errorf("loading controller database: %w", err)
	}
	return nil
}

// LoadModelDump loads one archived model database dump into the model's
// freshly created database (schema applied by the caller's opener).
func LoadModelDump(ctx context.Context, db *sql.DB, dump *Dump) error {
	if err := checkDumpVersion(dump, domainexport.ExportVersions); err != nil {
		return errors.Errorf("model dump: %w", err)
	}
	if err := loadDatabase(ctx, db, dump, modelTablePolicies, false); err != nil {
		return errors.Errorf("loading model database: %w", err)
	}
	return nil
}

// checkDumpVersion enforces the loader's native dump formats: a dump is
// only ever loaded when its envelope version names an export format this
// binary produces. The agent-version gate implies matching formats, but
// the envelope is the actual contract and is checked where the load runs.
func checkDumpVersion(dump *Dump, supported []semversion.Number) error {
	if dump.Version == "" {
		return errors.Errorf("dump records no format version")
	}
	parsed, err := semversion.Parse(dump.Version)
	if err != nil {
		return errors.Errorf("dump records unparseable format version %q", dump.Version)
	}
	if !slices.Contains(supported, parsed) {
		return errors.Errorf("unsupported dump format version %q: this loader reads %v", dump.Version, supported)
	}
	return nil
}

// loadDatabase replays dump tables into db under a single connection and
// transaction. Foreign key enforcement is disabled while loading so table
// order does not matter, then foreign_key_check proves integrity before
// the load is committed to.
func loadDatabase(ctx context.Context, db *sql.DB, dump *Dump, policies map[string]tablePolicy, controllerDB bool) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return errors.Capture(err)
	}
	defer func() { _ = conn.Close() }()

	if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		return errors.Errorf("disabling foreign keys: %w", err)
	}

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return errors.Capture(err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	tables := make([]string, 0, len(dump.Tables))
	for table := range dump.Tables {
		tables = append(tables, table)
	}
	sort.Strings(tables)

	for _, table := range tables {
		if err := ctx.Err(); err != nil {
			return errors.Capture(err)
		}
		if specialTables[table] {
			continue
		}
		switch policies[table] {
		case policySkip:
			continue
		case policyMerge:
			if err := insertRows(ctx, tx, table, dump.Tables[table], true); err != nil {
				return errors.Capture(err)
			}
		case policyInsert:
			if err := insertRows(ctx, tx, table, dump.Tables[table], false); err != nil {
				return errors.Capture(err)
			}
		default:
			return errors.Errorf("table %q has no valid restore policy", table)
		}
	}

	if controllerDB {
		if err := loadControllerRow(ctx, tx, dump.Tables["controller"]); err != nil {
			return errors.Capture(err)
		}
	}
	// Placements are rebuilt for the single replacement node in every
	// database: archived placements reference the source's node ids.
	if err := rebuildObjectStorePlacements(ctx, tx); err != nil {
		return errors.Capture(err)
	}

	// Prove integrity before committing: foreign_key_check inside the
	// transaction sees the uncommitted load, and a violation rolls back.
	violations, err := foreignKeyViolations(ctx, tx)
	if err != nil {
		return errors.Capture(err)
	}
	if len(violations) > 0 {
		return errors.Errorf("loaded database fails foreign key integrity: %s", strings.Join(violations, "; "))
	}

	if err := tx.Commit(); err != nil {
		return errors.Errorf("committing load: %w", err)
	}
	committed = true

	// The load is committed: a failure to re-enable foreign key
	// enforcement on this pooled connection does not invalidate it, so
	// it is not fatal here. Workers opening the database afterwards
	// establish their own connection pragmas.
	_, _ = conn.ExecContext(ctx, "PRAGMA foreign_keys = ON")
	return nil
}

// insertRows inserts every row into table. Column order is sorted for
// deterministic statements; merge selects INSERT OR IGNORE.
func insertRows(ctx context.Context, tx *sql.Tx, table string, rows []Row, merge bool) error {
	if len(rows) == 0 {
		return nil
	}
	verb := "INSERT"
	if merge {
		verb = "INSERT OR IGNORE"
	}
	for i, row := range rows {
		cols := make([]string, 0, len(row))
		for col := range row {
			cols = append(cols, col)
		}
		sort.Strings(cols)

		var quoted strings.Builder
		quoted.WriteString(verb + ` INTO "` + table + `" ("`)
		placeholders := make([]string, 0, len(cols))
		args := make([]any, 0, len(cols))
		for j, col := range cols {
			if j > 0 {
				quoted.WriteString(`", "`)
			}
			quoted.WriteString(col)
			placeholders = append(placeholders, "?")
			arg, err := normalizeValue(row[col])
			if err != nil {
				return errors.Errorf("table %q row %d column %q: %w", table, i, col, err)
			}
			args = append(args, arg)
		}
		quoted.WriteString(`") VALUES (` + strings.Join(placeholders, ", ") + `)`)

		if _, err := tx.ExecContext(ctx, quoted.String(), args...); err != nil {
			return errors.Errorf("table %q row %d: %w", table, i, err)
		}
	}
	return nil
}

// normalizeValue converts YAML-decoded values to driver values: booleans
// to integers (SQLite has no boolean type). Binary column values — BLOB
// storage in the source database — appear in dumps in two shapes:
// []byte-typed export fields marshal as sequences of integers, and
// string-typed fields holding binary marshal as !!binary, which the
// YAML decoder returns as a string of raw bytes. Both are bound back as
// bytes so the storage class survives the round trip and byte-expecting
// scanners keep working.
func normalizeValue(v any) (any, error) {
	switch t := v.(type) {
	case nil:
		return nil, nil
	case bool:
		if t {
			return int64(1), nil
		}
		return int64(0), nil
	case string:
		if !utf8.ValidString(t) {
			return []byte(t), nil
		}
		return t, nil
	case int:
		return int64(t), nil
	case []byte:
		return t, nil
	case []any:
		out := make([]byte, 0, len(t))
		for i, e := range t {
			n, ok := e.(int)
			if !ok || n < 0 || n > 255 {
				return nil, errors.Errorf("decoding byte sequence: element %d is not a byte (%T)", i, e)
			}
			out = append(out, byte(n))
		}
		return out, nil
	case int64, float64, time.Time:
		return t, nil
	default:
		return nil, errors.Errorf("unsupported value type %T", v)
	}
}

// loadControllerRow replaces the replacement's bootstrapped controller
// row with the source's row, preserving the source controller identity,
// CA and key material. Nothing references controller.uuid, so the
// replacement is a delete followed by the archived insert.
func loadControllerRow(ctx context.Context, tx *sql.Tx, rows []Row) error {
	if len(rows) == 0 {
		return errors.Errorf("controller dump has no controller row")
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM controller"); err != nil {
		return errors.Errorf("replacing bootstrapped controller row: %w", err)
	}
	if err := insertRows(ctx, tx, "controller", rows, false); err != nil {
		return errors.Capture(err)
	}
	return nil
}

// rebuildObjectStorePlacements rewrites object placements for the single
// replacement node: archived placements reference the source's node ids,
// which mean nothing here. Every metadata row is placed on the node id the
// replacement bootstrapped with; on a freshly created database that node
// id defaults to "0", the bootstrap controller node id.
func rebuildObjectStorePlacements(ctx context.Context, tx *sql.Tx) error {
	var nodeID string
	err := tx.QueryRowContext(ctx,
		"SELECT node_id FROM object_store_placement LIMIT 1").Scan(&nodeID)
	if errors.Is(err, sql.ErrNoRows) {
		nodeID = "0"
	} else if err != nil {
		return errors.Errorf("reading replacement object store node: %w", err)
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM object_store_placement"); err != nil {
		return errors.Errorf("clearing object store placements: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO object_store_placement (uuid, node_id) SELECT uuid, ? FROM object_store_metadata",
		nodeID); err != nil {
		return errors.Errorf("rebuilding object store placements: %w", err)
	}
	return nil
}

// foreignKeyViolations runs PRAGMA foreign_key_check and formats up to
// ten violations.
func foreignKeyViolations(ctx context.Context, tx *sql.Tx) ([]string, error) {
	rows, err := tx.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return nil, errors.Errorf("checking foreign keys: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var violations []string
	for rows.Next() {
		var table, parent string
		var rowid, fkid any
		if err := rows.Scan(&table, &rowid, &parent, &fkid); err != nil {
			return nil, errors.Errorf("reading foreign key violation: %w", err)
		}
		violations = append(violations, table+" row referencing "+parent)
		if len(violations) == 10 {
			violations = append(violations, "...")
			break
		}
	}
	return violations, errors.Capture(rows.Err())
}
