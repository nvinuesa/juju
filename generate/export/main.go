// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

//go:generate go run .

package main

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"go/format"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"text/template"

	"github.com/canonical/sqlair"
	_ "github.com/mattn/go-sqlite3"

	"github.com/juju/juju/core/semversion"
	"github.com/juju/juju/core/version"
	"github.com/juju/juju/domain/export"
	"github.com/juju/juju/domain/schema"
	"github.com/juju/juju/internal/database"
	"github.com/juju/juju/internal/logger"
)

// txnRunner is the simplest possible implementation of
// [core.database.TxnRunner]. It is used here to run database
// migrations and query schema metadata.
type txnRunner struct {
	db *sql.DB
}

func (r *txnRunner) Txn(ctx context.Context, f func(context.Context, *sqlair.TX) error) error {
	return database.Txn(ctx, sqlair.NewDB(r.db), f)
}

func (r *txnRunner) StdTxn(ctx context.Context, f func(context.Context, *sql.Tx) error) error {
	return database.StdTxn(ctx, r.db, f)
}

func (r *txnRunner) Dying() <-chan struct{} {
	return nil
}

func main() {
	fmt.Printf("Juju version: %s\n", version.Current)

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	runner := &txnRunner{db: db}
	m := database.NewDBMigration(runner, logger.Noop(), schema.ModelDDLForVersion(version.Current))

	ctx := context.Background()
	if err := m.Apply(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "failed to apply migration: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Applied model schema.")

	if err := generate(ctx, runner); err != nil {
		fmt.Fprintf(os.Stderr, "failed to generate schema: %v\n", err)
		os.Exit(1)
	}

	// Controller pass: a separate in-memory DB with the controller schema.
	ctrlDB, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open controller database: %v\n", err)
		os.Exit(1)
	}
	defer ctrlDB.Close()

	ctrlRunner := &txnRunner{db: ctrlDB}
	cm := database.NewDBMigration(ctrlRunner, logger.Noop(), schema.ControllerDDLForVersion(version.Current))
	if err := cm.Apply(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "failed to apply controller migration: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Applied controller schema.")

	if err := generateController(ctx, ctrlRunner); err != nil {
		fmt.Fprintf(os.Stderr, "failed to generate controller export: %v\n", err)
		os.Exit(1)
	}
}

func generate(ctx context.Context, runner *txnRunner) error {
	if len(export.ExportVersions) == 0 {
		return fmt.Errorf("no export versions defined")
	}
	semanticVersion := slices.MaxFunc(export.ExportVersions, semversion.Number.Compare).String()

	// Transform dots to underscores for use in package and directory names.
	versionToken := strings.ReplaceAll(semanticVersion, ".", "_")

	tableNames, err := getTableNames(ctx, runner)
	if err != nil {
		return err
	}

	var structs, structNames, usedTableNames []string
	imports := make(map[string]struct{})

	for _, tableName := range tableNames {
		if tableName == "sqlite_sequence" {
			continue
		}

		columns, err := getTableSchema(ctx, runner, tableName)
		if err != nil {
			return err
		}

		structDef, requiredImports, err := generateStruct(tableName, columns)
		if err != nil {
			return err
		}

		structs = append(structs, structDef)
		structNames = append(structNames, toCamelCase(tableName))
		usedTableNames = append(usedTableNames, tableName)
		for _, imp := range requiredImports {
			imports[imp] = struct{}{}
		}
	}

	if err := writeTypesFile(versionToken, usedTableNames, structs, structNames, imports); err != nil {
		return err
	}

	if err := writeStateModelVersionFile(versionToken, semanticVersion, usedTableNames, structNames); err != nil {
		return err
	}

	if err := writeServiceModelVersionFile(versionToken, semanticVersion); err != nil {
		return err
	}

	// The restore importers are generated from the same schema as the export
	// payloads; they are the write-mirror of the export state.
	if err := generateRestoreImport(ctx, runner, versionToken, semanticVersion, modelRestoreSkipTables, "model", "ModelExport"); err != nil {
		return err
	}

	return generateTransforms(exportVersionStrings(export.ExportVersions))
}

// generateController mirrors generate() for the controller schema. Transforms
// stay model-only: the controller payload has no version history and its only
// consumer is the backup feature.
func generateController(ctx context.Context, runner *txnRunner) error {
	if len(export.ControllerExportVersions) == 0 {
		return fmt.Errorf("no controller export versions defined")
	}
	semanticVersion := slices.MaxFunc(
		export.ControllerExportVersions, semversion.Number.Compare).String()

	// Transform dots to underscores for use in package and directory names.
	versionToken := strings.ReplaceAll(semanticVersion, ".", "_")

	tableNames, err := getTableNames(ctx, runner)
	if err != nil {
		return err
	}

	var structs, structNames, usedTableNames []string
	imports := make(map[string]struct{})

	for _, tableName := range tableNames {
		if tableName == "sqlite_sequence" {
			continue
		}

		columns, err := getTableSchema(ctx, runner, tableName)
		if err != nil {
			return err
		}

		structDef, requiredImports, err := generateStruct(tableName, columns)
		if err != nil {
			return err
		}

		structs = append(structs, structDef)
		structNames = append(structNames, toCamelCase(tableName))
		usedTableNames = append(usedTableNames, tableName)
		for _, imp := range requiredImports {
			imports[imp] = struct{}{}
		}
	}

	if err := writeControllerTypesFile(versionToken, usedTableNames, structs, structNames, imports); err != nil {
		return err
	}

	if err := writeControllerStateFile(versionToken, semanticVersion, usedTableNames, structNames); err != nil {
		return err
	}

	if err := writeControllerServiceFile(versionToken, semanticVersion); err != nil {
		return err
	}

	// The restore importers are generated from the same schema as the export
	// payloads; they are the write-mirror of the export state.
	return generateRestoreImport(ctx, runner, versionToken, semanticVersion, controllerRestoreSkipTables, "controller", "ControllerExport")
}

func exportVersionStrings(versions []semversion.Number) []string {
	result := make([]string, len(versions))
	for i, v := range versions {
		result[i] = v.String()
	}
	return result
}

func getTableNames(ctx context.Context, runner *txnRunner) ([]string, error) {
	var tableNames []string
	err := runner.StdTxn(ctx, func(ctx context.Context, tx *sql.Tx) error {
		tableNames = nil

		rows, err := tx.QueryContext(ctx, "SELECT name FROM sqlite_master WHERE type='table'")
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return err
			}
			tableNames = append(tableNames, name)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(tableNames)
	return tableNames, nil
}

type column struct {
	Name    string
	Type    string
	NotNull bool
}

func getTableSchema(ctx context.Context, runner *txnRunner, tableName string) ([]column, error) {
	var columns []column
	query := fmt.Sprintf("PRAGMA table_info(%q)", tableName)
	err := runner.StdTxn(ctx, func(ctx context.Context, tx *sql.Tx) error {
		columns = nil

		rows, err := tx.QueryContext(ctx, query)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var cid int
			var name, typ, defaultVal sql.NullString
			var notnull, pk int
			if err := rows.Scan(&cid, &name, &typ, &notnull, &defaultVal, &pk); err != nil {
				return err
			}
			columns = append(columns, column{
				Name:    name.String,
				Type:    typ.String,
				NotNull: notnull != 0,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return columns, nil
}

// toCamelCase converts snake case identifiers from the database to
// camel case identifiers for Go types.
// Exceptions are made for "id" and "uuid", which become all caps.
func toCamelCase(s string) string {
	if s == "" {
		return ""
	}

	parts := strings.Split(s, "_")
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		switch strings.ToLower(p) {
		case "id":
			b.WriteString("ID")
		case "uuid":
			b.WriteString("UUID")
		default:
			l := strings.ToLower(p)
			b.WriteString(strings.ToUpper(l[:1]) + l[1:])
		}
	}

	return b.String()
}

func generateStruct(tableName string, columns []column) (string, []string, error) {
	structName := toCamelCase(tableName)
	var sb strings.Builder

	if _, err := sb.WriteString(fmt.Sprintf("type %s struct {\n", structName)); err != nil {
		return "", nil, err
	}

	var imports []string
	for _, col := range columns {
		goType, imp := sqliteTypeToGoType(col.Type, col.NotNull)
		if tableName == "bakery_config" ||
			tableName == "macaroon_root_key" && col.Name == "root_key" {
			goType = "Binary"
		}
		if imp != "" {
			imports = append(imports, imp)
		}
		fieldName := toCamelCase(col.Name)

		if _, err := sb.WriteString(
			fmt.Sprintf(
				"\t%s %s `db:%q json:%q yaml:%q`\n",
				fieldName,
				goType,
				col.Name,
				col.Name,
				col.Name,
			),
		); err != nil {
			return "", nil, err
		}
	}

	if _, err := sb.WriteString("}\n"); err != nil {
		return "", nil, err
	}

	return sb.String(), imports, nil
}

func sqliteTypeToGoType(sqliteType string, notNull bool) (string, string) {
	var goType, imp string

	switch strings.ToUpper(sqliteType) {
	case "INTEGER", "INT":
		goType = "int64"
	case "TEXT":
		goType = "string"
	case "BOOLEAN":
		goType = "bool"
	case "DATETIME", "TIMESTAMP":
		goType = "time.Time"
		imp = "time"
	case "BLOB":
		goType = "[]byte"
	default:
		goType = "any"
	}

	if !notNull {
		goType = "*" + goType
	}
	return goType, imp
}

func writeTypesFile(
	version string,
	tableNames []string,
	structs []string,
	structNames []string,
	imports map[string]struct{},
) error {
	// We should be in domain/export/generate.
	_, filename, _, _ := runtime.Caller(0)
	currentDir := filepath.Dir(filename)

	// Target directory is always under the repository's domain/export path.
	repoRoot := filepath.Dir(filepath.Dir(currentDir)) // generate/export -> generate -> repo root
	dir := filepath.Join(repoRoot, "domain", "export", "types", fmt.Sprintf("v%s", version))

	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Prepare import slice sorted for stable output.
	sortedImports := make([]string, 0, len(imports))
	for imp := range imports {
		sortedImports = append(sortedImports, imp)
	}
	sort.Strings(sortedImports)

	tmplPath := filepath.Join(filepath.Dir(filename), "types.tmpl")
	tmplBytes, err := os.ReadFile(tmplPath)
	if err != nil {
		return err
	}

	data := struct {
		Version     string
		Imports     []string
		TableNames  []string
		Structs     []string
		StructNames []string
	}{
		Version:     version,
		Imports:     sortedImports,
		TableNames:  tableNames,
		Structs:     structs,
		StructNames: structNames,
	}

	t := template.Must(template.New("types").Parse(string(tmplBytes)))
	var out bytes.Buffer
	if err := t.Execute(&out, data); err != nil {
		return err
	}

	formatted, err := format.Source(out.Bytes())
	if err != nil {
		return err
	}

	filePath := filepath.Join(dir, "model.go")
	fmt.Printf("writing to %s\n", filePath)
	return os.WriteFile(filePath, formatted, 0644)
}

func writeStateModelVersionFile(
	versionToken string,
	semanticVersion string,
	tableNames []string,
	structNames []string,
) error {
	_, filename, _, _ := runtime.Caller(0)
	currentDir := filepath.Dir(filename)

	repoRoot := filepath.Dir(filepath.Dir(currentDir))
	dir := filepath.Join(repoRoot, "domain", "export", "state", "model")

	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	tmplPath := filepath.Join(filepath.Dir(filename), "state.tmpl")
	tmplBytes, err := os.ReadFile(tmplPath)
	if err != nil {
		return err
	}

	data := struct {
		VersionToken    string
		SemanticVersion string
		TableNames      []string
		StructNames     []string
	}{
		VersionToken:    versionToken,
		SemanticVersion: semanticVersion,
		TableNames:      tableNames,
		StructNames:     structNames,
	}

	t := template.Must(template.New("state").Parse(string(tmplBytes)))
	var out bytes.Buffer
	if err := t.Execute(&out, data); err != nil {
		return err
	}

	formatted, err := format.Source(out.Bytes())
	if err != nil {
		log.Printf("error formatting generated code for v%s.go: %v", versionToken, err)
		formatted = out.Bytes()
	}

	// Write to stable filenames export.go and export_test.go so the state package
	// always contains the latest export logic.
	filePath := filepath.Join(dir, "export.go")
	fmt.Printf("writing to %s\n", filePath)
	if err := os.WriteFile(filePath, formatted, 0644); err != nil {
		return err
	}

	// Also generate a basic test that runs the ExportV<version> method against
	// the real model DB, written to export_test.go.
	testTmplPath := filepath.Join(filepath.Dir(filename), "state_test.tmpl")
	testTmplBytes, err := os.ReadFile(testTmplPath)
	if err != nil {
		return err
	}

	testData := struct {
		VersionToken string
	}{
		VersionToken: versionToken,
	}

	testT := template.Must(template.New("state_test").Parse(string(testTmplBytes)))
	var testOut bytes.Buffer
	if err := testT.Execute(&testOut, testData); err != nil {
		return err
	}
	testFormatted, err := format.Source(testOut.Bytes())
	if err != nil {
		return err
	}

	testFilePath := filepath.Join(dir, "export_test.go")
	fmt.Printf("writing to %s\n", testFilePath)
	if err := os.WriteFile(testFilePath, testFormatted, 0644); err != nil {
		return err
	}

	return nil
}

func writeServiceModelVersionFile(versionToken, semanticVersion string) error {
	_, filename, _, _ := runtime.Caller(0)
	currentDir := filepath.Dir(filename)

	repoRoot := filepath.Dir(filepath.Dir(currentDir))
	dir := filepath.Join(repoRoot, "domain", "export", "service")

	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	tmplPath := filepath.Join(filepath.Dir(filename), "service.tmpl")
	tmplBytes, err := os.ReadFile(tmplPath)
	if err != nil {
		return err
	}

	data := struct {
		VersionToken    string
		SemanticVersion string
	}{
		VersionToken:    versionToken,
		SemanticVersion: semanticVersion,
	}

	t := template.Must(template.New("service").Parse(string(tmplBytes)))
	var out bytes.Buffer
	if err := t.Execute(&out, data); err != nil {
		return err
	}

	formatted, err := format.Source(out.Bytes())
	if err != nil {
		return err
	}

	filePath := filepath.Join(dir, "export.go")
	fmt.Printf("writing to %s\n", filePath)
	if err := os.WriteFile(filePath, formatted, 0644); err != nil {
		return err
	}

	testTmplPath := filepath.Join(filepath.Dir(filename), "service_test.tmpl")
	testTmplBytes, err := os.ReadFile(testTmplPath)
	if err != nil {
		return err
	}

	testT := template.Must(template.New("service_test").Parse(string(testTmplBytes)))
	var testOut bytes.Buffer
	if err := testT.Execute(&testOut, data); err != nil {
		return err
	}

	testFormatted, err := format.Source(testOut.Bytes())
	if err != nil {
		return err
	}

	testFilePath := filepath.Join(dir, "export_test.go")
	fmt.Printf("writing to %s\n", testFilePath)
	return os.WriteFile(testFilePath, testFormatted, 0644)
}

// writeControllerTypesFile emits the aggregate controller-export payload type
// under domain/export/types/controller/v<token>/controller.go.
func writeControllerTypesFile(
	version string,
	tableNames []string,
	structs []string,
	structNames []string,
	imports map[string]struct{},
) error {
	_, filename, _, _ := runtime.Caller(0)
	currentDir := filepath.Dir(filename)
	repoRoot := filepath.Dir(filepath.Dir(currentDir))
	dir := filepath.Join(repoRoot, "domain", "export", "types", "controller", fmt.Sprintf("v%s", version))

	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	sortedImports := make([]string, 0, len(imports))
	for imp := range imports {
		sortedImports = append(sortedImports, imp)
	}
	sort.Strings(sortedImports)

	tmplBytes, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "controller_types.tmpl"))
	if err != nil {
		return err
	}

	data := struct {
		Version     string
		Imports     []string
		TableNames  []string
		Structs     []string
		StructNames []string
	}{
		Version:     version,
		Imports:     sortedImports,
		TableNames:  tableNames,
		Structs:     structs,
		StructNames: structNames,
	}

	t := template.Must(template.New("controller_types").Parse(string(tmplBytes)))
	var out bytes.Buffer
	if err := t.Execute(&out, data); err != nil {
		return err
	}

	formatted, err := format.Source(out.Bytes())
	if err != nil {
		return err
	}

	filePath := filepath.Join(dir, "controller.go")
	fmt.Printf("writing to %s\n", filePath)
	return os.WriteFile(filePath, formatted, 0644)
}

// writeControllerStateFile emits the controller export state into
// domain/export/state/controller/export.go plus its smoke test.
func writeControllerStateFile(
	versionToken string,
	semanticVersion string,
	tableNames []string,
	structNames []string,
) error {
	_, filename, _, _ := runtime.Caller(0)
	currentDir := filepath.Dir(filename)
	repoRoot := filepath.Dir(filepath.Dir(currentDir))
	dir := filepath.Join(repoRoot, "domain", "export", "state", "controller")

	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	tmplBytes, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "controller_state.tmpl"))
	if err != nil {
		return err
	}

	data := struct {
		VersionToken    string
		SemanticVersion string
		TableNames      []string
		StructNames     []string
	}{
		VersionToken:    versionToken,
		SemanticVersion: semanticVersion,
		TableNames:      tableNames,
		StructNames:     structNames,
	}

	t := template.Must(template.New("controller_state").Parse(string(tmplBytes)))
	var out bytes.Buffer
	if err := t.Execute(&out, data); err != nil {
		return err
	}

	formatted, err := format.Source(out.Bytes())
	if err != nil {
		log.Printf("error formatting generated controller state: %v", err)
		formatted = out.Bytes()
	}

	filePath := filepath.Join(dir, "export.go")
	fmt.Printf("writing to %s\n", filePath)
	if err := os.WriteFile(filePath, formatted, 0644); err != nil {
		return err
	}

	testTmplBytes, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "controller_state_test.tmpl"))
	if err != nil {
		return err
	}

	testT := template.Must(template.New("controller_state_test").Parse(string(testTmplBytes)))
	var testOut bytes.Buffer
	if err := testT.Execute(&testOut, data); err != nil {
		return err
	}
	testFormatted, err := format.Source(testOut.Bytes())
	if err != nil {
		return err
	}

	testFilePath := filepath.Join(dir, "export_test.go")
	fmt.Printf("writing to %s\n", testFilePath)
	return os.WriteFile(testFilePath, testFormatted, 0644)
}

// writeControllerServiceFile emits the controller export service into
// domain/export/service/controller_export.go plus its test.
func writeControllerServiceFile(versionToken, semanticVersion string) error {
	_, filename, _, _ := runtime.Caller(0)
	currentDir := filepath.Dir(filename)
	repoRoot := filepath.Dir(filepath.Dir(currentDir))
	dir := filepath.Join(repoRoot, "domain", "export", "service")

	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	tmplBytes, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "controller_service.tmpl"))
	if err != nil {
		return err
	}

	data := struct {
		VersionToken    string
		SemanticVersion string
	}{
		VersionToken:    versionToken,
		SemanticVersion: semanticVersion,
	}

	t := template.Must(template.New("controller_service").Parse(string(tmplBytes)))
	var out bytes.Buffer
	if err := t.Execute(&out, data); err != nil {
		return err
	}

	formatted, err := format.Source(out.Bytes())
	if err != nil {
		return err
	}

	filePath := filepath.Join(dir, "controller_export.go")
	fmt.Printf("writing to %s\n", filePath)
	if err := os.WriteFile(filePath, formatted, 0644); err != nil {
		return err
	}

	testTmplBytes, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "controller_service_test.tmpl"))
	if err != nil {
		return err
	}

	testT := template.Must(template.New("controller_service_test").Parse(string(testTmplBytes)))
	var testOut bytes.Buffer
	if err := testT.Execute(&testOut, data); err != nil {
		return err
	}

	testFormatted, err := format.Source(testOut.Bytes())
	if err != nil {
		return err
	}

	testFilePath := filepath.Join(dir, "controller_export_test.go")
	fmt.Printf("writing to %s\n", testFilePath)
	return os.WriteFile(testFilePath, testFormatted, 0644)
}

// controllerRestoreSkipTables are controller-DB tables the generated
// restore-import importer must not populate from the source payload:
//
//   - schema: the target schema/migration history is preserved; the source
//     schema rows are never imported.
//   - changelog tables: the target changestream starts fresh; both
//     change_log and change_log_witness are cleared at the end of restore.
//   - namespace_list: made authoritative at the end of restore, not imported.
//   - target-local node/config tables: controller node 0, its addresses,
//     password, agent version, SSH host key, and runtime config are captured
//     from the target and overlaid after import; the source rows describe a
//     different controller.
//   - object-store backend/drain: rejected by preflight (any S3 backend or
//     active drain makes the source archive unsupported), and the seeded
//     file-backend row is target-side; object_store_placement is rewritten
//     to node 0 by the overlay. The object_store_metadata rows are NOT
//     skipped: they are source logical data FK-referenced by model blobs and
//     controller placement, so they must be bulk-imported.
//   - secret backend tables: source rows are rejected by preflight unless
//     every backend is a builtin (origin_id 0); builtins stay target-side.
//
// NOTE: this list is the restore import-exclusion contract. The overlay
// applies target-local values for the skipped tables.
var controllerRestoreSkipTables = map[string]bool{
	"schema":                         true,
	"change_log":                     true,
	"change_log_witness":             true,
	"change_log_edit_type":           true,
	"change_log_namespace":           true,
	"namespace_list":                 true,
	"controller":                     true,
	"controller_config":              true,
	"controller_node":                true,
	"controller_node_agent_version":  true,
	"controller_node_password":       true,
	"controller_api_address":         true,
	"controller_ssh_host_key":        true,
	"object_store_backend":           true,
	"object_store_backend_s3_config": true,
	"object_store_drain_info":        true,
	"object_store_drain_phase_type":  true,
	"object_store_placement":         true,
	"secret_backend":                 true,
	"secret_backend_config":          true,
	"secret_backend_reference":       true,
	"secret_backend_rotation":        true,
	"model_secret_backend":           true,
}

// modelRestoreSkipTables are model-DB tables the generated restore importer
// must not populate from the source payload:
//
//   - changelog tables: the target changestream starts fresh.
//   - object_store_placement: rewritten to node 0 by the model overlay; the
//     object_store_metadata rows are NOT skipped: they are source logical
//     data FK-referenced by charms/resources/agent binaries.
//
// Unlike the migration model importer, the bootstrap identity tables (model,
// model_life, agent_version, model_migrating) ARE imported: restore replaces
// the temporary controller model and creates fresh model databases from
// source content. model_agent is also imported so the model overlay can
// merge the target-local password hash onto the restored row.
//
// NOTE: model DBs are created fresh per namespace, so every other table is a
// clean bulk insert.
var modelRestoreSkipTables = map[string]bool{
	"change_log":             true,
	"change_log_witness":     true,
	"change_log_edit_type":   true,
	"change_log_namespace":   true,
	"object_store_placement": true,
}

// restoreImportTableData describes one table to bulk-import.
type restoreImportTableData struct {
	StructName string
	TableName  string
	Seeded     bool
}

// generateRestoreImport emits the restore import state into
// domain/restoreimport/state/<dirName>/import.go plus its smoke test. The
// importer bulk-inserts all tables except skipped ones; seeded tables are
// inserted with ON CONFLICT DO NOTHING, non-seeded tables are wiped first.
func generateRestoreImport(ctx context.Context, runner *txnRunner, versionToken, semanticVersion string, skip map[string]bool, dirName, payloadType string) error {
	tableNames, err := getTableNames(ctx, runner)
	if err != nil {
		return err
	}
	seeded, err := getSeededTables(ctx, runner, tableNames)
	if err != nil {
		return err
	}

	var tables []restoreImportTableData
	for _, tableName := range tableNames {
		if tableName == "sqlite_sequence" {
			continue
		}
		if skip[tableName] {
			continue
		}
		tables = append(tables, restoreImportTableData{
			StructName: toCamelCase(tableName),
			TableName:  tableName,
			Seeded:     seeded[tableName],
		})
	}

	return writeRestoreImportFiles(filepath.Join("domain", "restoreimport", "state", dirName),
		dirName, versionToken, semanticVersion, payloadType, tables)
}

// getSeededTables reports which tables have rows in the freshly applied
// schema (i.e. are seeded by DDL).
func getSeededTables(ctx context.Context, runner *txnRunner, tableNames []string) (map[string]bool, error) {
	var seeded map[string]bool
	err := runner.StdTxn(ctx, func(ctx context.Context, tx *sql.Tx) error {
		seeded = make(map[string]bool)
		for _, tableName := range tableNames {
			var count int
			query := fmt.Sprintf("SELECT COUNT(*) FROM %q", tableName)
			if err := tx.QueryRowContext(ctx, query).Scan(&count); err != nil {
				return err
			}
			if count > 0 {
				seeded[tableName] = true
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return seeded, nil
}

// writeRestoreImportFiles renders the restore import state file and its
// smoke test into the package directory at the given repo-relative path.
func writeRestoreImportFiles(dir, packageName, versionToken, semanticVersion, payloadType string, tables []restoreImportTableData) error {
	_, filename, _, _ := runtime.Caller(0)
	currentDir := filepath.Dir(filename)
	repoRoot := filepath.Dir(filepath.Dir(currentDir))
	targetDir := filepath.Join(repoRoot, dir)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return err
	}

	data := struct {
		Package         string
		VersionToken    string
		SemanticVersion string
		PayloadType     string
		Tables          []restoreImportTableData
	}{
		Package:         packageName,
		VersionToken:    versionToken,
		SemanticVersion: semanticVersion,
		PayloadType:     payloadType,
		Tables:          tables,
	}

	if err := renderRestoreImportTemplate(filepath.Join(currentDir, "restoreimport.tmpl"), filepath.Join(targetDir, "import.go"), "restoreimport", data); err != nil {
		return err
	}
	if err := renderRestoreImportTemplate(filepath.Join(currentDir, "restoreimport_test.tmpl"), filepath.Join(targetDir, "import_test.go"), "restoreimport_test", data); err != nil {
		return err
	}
	return nil
}

// renderRestoreImportTemplate executes a template file and writes the
// gofmt-formatted result to the output path.
func renderRestoreImportTemplate(tmplPath, outPath, name string, data any) error {
	tmplBytes, err := os.ReadFile(tmplPath)
	if err != nil {
		return err
	}
	t := template.Must(template.New(name).Parse(string(tmplBytes)))
	var out bytes.Buffer
	if err := t.Execute(&out, data); err != nil {
		return err
	}
	formatted, err := format.Source(out.Bytes())
	if err != nil {
		return err
	}
	fmt.Printf("writing to %s\n", outPath)
	return os.WriteFile(outPath, formatted, 0644)
}
