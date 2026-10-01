// Copyright 2022 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package database

import (
	"context"
	"database/sql"

	"github.com/canonical/sqlair"
	"github.com/juju/errors"

	coredatabase "github.com/juju/juju/core/database"
	"github.com/juju/juju/core/logger"
	"github.com/juju/juju/core/model"
	"github.com/juju/juju/core/network"
	"github.com/juju/juju/domain/schema"
	"github.com/juju/juju/internal/database/app"
	"github.com/juju/juju/internal/database/pragma"
)

// BootstrapNodeManager is an interface for managing the bootstrap of a Dqlite
// node.
type BootstrapNodeManager interface {
	// EnsureDataDir ensures that a directory for Dqlite data exists at
	// a path determined by the agent config, then returns that path.
	EnsureDataDir() (string, error)

	// WithAddressOption returns a Dqlite application Option for binding to the
	// supplied address.
	WithAddressOption(string) app.Option

	// WithTLSOption returns a Dqlite application Option for TLS encryption
	// of traffic between clients and clustered application nodes.
	WithTLSOption() (app.Option, error)

	// WithLogFuncOption returns a Dqlite application Option
	// that will proxy Dqlite log output via this factory's
	// logger where the level is recognised.
	WithLogFuncOption() app.Option

	// WithTracingOption returns a Dqlite application Option
	// that will enable tracing of Dqlite operations.
	WithTracingOption() app.Option
}

// BootstrapOpt is a function run when bootstrapping a database,
// used to insert initial data into the model.
type BootstrapOpt func(
	ctx context.Context,
	controller, model coredatabase.TxnRunner,
) error

// RestoreStage runs after the controller and controller-model databases
// are migrated and before the bootstrap seed operations and app shutdown.
// It receives the open dqlite app so the restore load can open every
// archived model database while bootstrap owns the databases exclusively.
type RestoreStage func(ctx context.Context, dqlite *app.App) error

// BootstrapDqlite opens a new database for the controller, and runs the
// DDL to create its schema.
//
// It accepts an optional list of functions to perform operations on the
// controller database.
func BootstrapDqlite(
	ctx context.Context,
	mgr BootstrapNodeManager,
	bootstrapAddresses network.ProviderAddresses,
	uuid model.UUID,
	logger logger.Logger,
	opts ...BootstrapOpt,
) error {
	return bootstrapDqlite(ctx, mgr, bootstrapAddresses, uuid, logger, nil, opts...)
}

// BootstrapDqliteWithRestore is BootstrapDqlite with a restore stage run
// after the migrations and before the seed operations. Restore mode skips
// the identity seed operations: the stage loads the archived databases
// instead.
func BootstrapDqliteWithRestore(
	ctx context.Context,
	mgr BootstrapNodeManager,
	bootstrapAddresses network.ProviderAddresses,
	uuid model.UUID,
	logger logger.Logger,
	stage RestoreStage,
	opts ...BootstrapOpt,
) error {
	return bootstrapDqlite(ctx, mgr, bootstrapAddresses, uuid, logger, stage, opts...)
}

func bootstrapDqlite(
	ctx context.Context,
	mgr BootstrapNodeManager,
	bootstrapAddresses network.ProviderAddresses,
	uuid model.UUID,
	logger logger.Logger,
	stage RestoreStage,
	opts ...BootstrapOpt,
) error {
	dir, err := mgr.EnsureDataDir()
	if err != nil {
		return errors.Trace(err)
	}

	address, ok := bootstrapAddresses.OneMatchingScope(network.ScopeMatchCloudLocal)
	if !ok {
		return errors.NotFoundf("Dqlite bootstrap address")
	}
	bindAddress := address.Value
	tlsOpt, err := mgr.WithTLSOption()
	if err != nil {
		return errors.Annotate(err, "generating TLS option")
	}
	options := []app.Option{
		mgr.WithLogFuncOption(),
		mgr.WithAddressOption(bindAddress),
		tlsOpt,
	}

	dqlite, err := app.New(dir, options...)
	if err != nil {
		return errors.Annotate(err, "creating Dqlite app")
	}
	defer func() {
		if err := dqlite.Close(); err != nil {
			logger.Errorf(ctx, "closing Dqlite: %v", err)
		}
	}()

	if err := dqlite.Ready(ctx); err != nil {
		return errors.Annotatef(err, "waiting for Dqlite readiness")
	}

	controller, err := runMigration(
		ctx, dqlite, coredatabase.ControllerNS, schema.ControllerDDL(),
		controllerBootstrapInit(bindAddress), logger,
	)
	if err != nil {
		return errors.Annotate(err, "running controller migration")
	}

	model, err := runMigration(ctx, dqlite, uuid.String(), schema.ModelDDL(), emptyInit, logger)
	if err != nil {
		return errors.Annotatef(err, "running model migration")
	}

	// The restore stage loads archived databases while the app is open
	// and bootstrap owns every database exclusively.
	if stage != nil {
		if err := stage(ctx, dqlite); err != nil {
			return errors.Annotatef(err, "running restore stage")
		}
	}

	for i, op := range opts {
		if err := op(ctx, controller, model); err != nil {
			return errors.Annotatef(err, "running bootstrap operation at index %d", i)
		}
	}

	return nil
}

// EnsureModelDatabase opens the database for the given namespace on the
// dqlite app, applying the model schema. Schema application is versioned
// and idempotent, so this is safe on both fresh and existing databases.
// The restore stage uses it to create the database of every archived
// model; the caller owns the returned handle.
func EnsureModelDatabase(ctx context.Context, dqlite *app.App, namespace string, logger logger.Logger) (*sql.DB, error) {
	db, err := dqlite.Open(ctx, namespace)
	if err != nil {
		return nil, errors.Annotatef(err, "opening database for namespace %q", namespace)
	}
	runner := &txnRunner{db: db}
	if err := NewDBMigration(runner, logger, schema.ModelDDL()).Apply(ctx); err != nil {
		_ = db.Close()
		return nil, errors.Annotatef(err, "migrating database with namespace %q schema", namespace)
	}
	return db, nil
}

func runMigration(ctx context.Context, dqlite *app.App, namespace string, schema Schema, init bootstrapInit, logger logger.Logger) (coredatabase.TxnRunner, error) {
	db, err := dqlite.Open(ctx, namespace)
	if err != nil {
		return nil, errors.Annotatef(err, "opening database for namespace %q", namespace)
	}

	if err := pragma.SetPragma(ctx, db, pragma.ForeignKeysPragma, true); err != nil {
		return nil, errors.Annotatef(err, "setting foreign keys pragma for namespace %q", namespace)
	}

	runner := &txnRunner{db: db}

	migration := NewDBMigration(runner, logger, schema)
	if err := migration.Apply(ctx); err != nil {
		return nil, errors.Annotatef(err, "creating database with namespace %q schema", namespace)
	}

	if err := init(ctx, runner, dqlite); err != nil {
		return nil, errors.Annotatef(err, "running init for database with namespace %q", namespace)
	}

	return runner, nil
}

// InsertControllerNodeID inserts the node ID of the controller node
// into the controller_node table.
func InsertControllerNodeID(
	ctx context.Context, runner coredatabase.TxnRunner, nodeID uint64, bindAddress string,
) error {
	q := `
-- TODO (manadart 2023-06-06): At the time of writing, 
-- we have not yet modelled machines. 
-- Accordingly, the controller ID remains the ID of the machine, 
-- but it should probably become a UUID once machines have one.
INSERT INTO controller_node (controller_id, dqlite_node_id, dqlite_bind_address)
VALUES ('0', ?, ?);`
	return runner.StdTxn(ctx, func(ctx context.Context, tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, q, nodeID, bindAddress)
		if err != nil {
			return errors.Trace(err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return errors.Trace(err)
		}
		if affected != 1 {
			return errors.Errorf("expected 1 row affected, got %d", affected)
		}
		return nil
	})
}

// txnRunner is the simplest implementation of TxnRunner, wrapping a
// sql.DB reference. It is recruited to run the bootstrap DB migration,
// where we do not yet have access to a transaction runner sourced from
// dbaccessor worker.
type txnRunner struct {
	db *sql.DB
}

func (r *txnRunner) Txn(ctx context.Context, f func(context.Context, *sqlair.TX) error) error {
	return errors.Trace(Txn(ctx, sqlair.NewDB(r.db), f))
}

func (r *txnRunner) StdTxn(ctx context.Context, f func(context.Context, *sql.Tx) error) error {
	return errors.Trace(StdTxn(ctx, r.db, f))
}

func (r *txnRunner) Dying() <-chan struct{} {
	return make(<-chan struct{})
}

// bootstrapInit is a type for describing a bootstrap operation that
// initialises a database.
type bootstrapInit = func(ctx context.Context, runner coredatabase.TxnRunner, dqlite *app.App) error

// controllerBootstrapInit is used to initialise the controller database with
// a controller node ID. The controller node ID is required to be present in
// the controller_node table as this is used for referential integrity.
func controllerBootstrapInit(bindAddress string) bootstrapInit {
	return func(ctx context.Context, runner coredatabase.TxnRunner, dqlite *app.App) error {
		if err := InsertControllerNodeID(ctx, runner, dqlite.ID(), bindAddress); err != nil {
			return errors.Annotatef(err, "inserting controller node ID")
		}
		return nil
	}
}

// emptyInit is a BootstrapInit type that does nothing.
func emptyInit(context.Context, coredatabase.TxnRunner, *app.App) error {
	return nil
}
