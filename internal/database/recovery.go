// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package database

import (
	"context"
	"database/sql"

	coredatabase "github.com/juju/juju/core/database"
	"github.com/juju/juju/core/logger"
	"github.com/juju/juju/core/model"
	"github.com/juju/juju/core/network"
	"github.com/juju/juju/domain/schema"
	"github.com/juju/juju/internal/database/app"
	"github.com/juju/juju/internal/errors"
)

// RecoveryStage runs after the controller and controller-model databases
// are migrated and before the bootstrap seed operations and app shutdown.
// It receives the open dqlite app so the recovery load can open every
// archived model database while bootstrap owns the databases exclusively.
type RecoveryStage func(ctx context.Context, dqlite *app.App) error

// RecoverDqlite opens a new database for the controller, and runs the
// DDL to create its schema, then runs the recovery stage after the
// migrations and before the seed operations. Recovery mode skips the
// identity seed operations: the stage loads the archived databases
// instead.
func RecoverDqlite(
	ctx context.Context,
	mgr BootstrapNodeManager,
	bootstrapAddresses network.ProviderAddresses,
	uuid model.UUID,
	logger logger.Logger,
	stage RecoveryStage,
	opts ...BootstrapOpt,
) error {
	return errors.Capture(WithDqlite(ctx, mgr, bootstrapAddresses, logger, func(ctx context.Context, session *DqliteSession) error {
		controller, err := session.OpenDatabase(ctx, coredatabase.ControllerNS, schema.ControllerDDL())
		if err != nil {
			return errors.Errorf("running controller migration: %w", err)
		}

		// The controller node must exist before the seed operations run,
		// as it is required for referential integrity.
		if err := InsertControllerNodeID(ctx, controller, session.NodeID(), session.BindAddress()); err != nil {
			return errors.Errorf("inserting controller node ID: %w", err)
		}

		model, err := session.OpenDatabase(ctx, uuid.String(), schema.ModelDDL())
		if err != nil {
			return errors.Errorf("running model migration: %w", err)
		}

		// The recovery stage loads archived databases while the app is
		// open and bootstrap owns every database exclusively.
		if stage != nil {
			dqlite, ok := session.app.(*app.App)
			if !ok {
				return errors.Errorf("recovery stage requires a dqlite app, got %T", session.app)
			}
			if err := stage(ctx, dqlite); err != nil {
				return errors.Errorf("running recovery stage: %w", err)
			}
		}

		for i, op := range opts {
			if err := op(ctx, controller, model); err != nil {
				return errors.Errorf("running bootstrap operation at index %d: %w", i, err)
			}
		}

		return nil
	}))
}

// EnsureModelDatabase opens the database for the given namespace on the
// dqlite app, applying the model schema. Schema application is versioned
// and idempotent, so this is safe on both fresh and existing databases.
// The recovery stage uses it to create the database of every archived
// model; the caller owns the returned handle.
func EnsureModelDatabase(ctx context.Context, dqlite *app.App, namespace string, logger logger.Logger) (*sql.DB, error) {
	db, err := dqlite.Open(ctx, namespace)
	if err != nil {
		return nil, errors.Errorf("opening database for namespace %q: %w", namespace, err)
	}
	runner := &txnRunner{db: db}
	if err := NewDBMigration(runner, logger, schema.ModelDDL()).Apply(ctx); err != nil {
		_ = db.Close()
		return nil, errors.Errorf("migrating database with namespace %q schema: %w", namespace, err)
	}
	return db, nil
}
