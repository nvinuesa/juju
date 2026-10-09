// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package database

import (
	"context"
	"testing"

	"github.com/juju/tc"

	"github.com/juju/juju/core/database"
	coremodel "github.com/juju/juju/core/model"
	"github.com/juju/juju/core/network"
	"github.com/juju/juju/internal/database/app"
	loggertesting "github.com/juju/juju/internal/logger/testing"
	"github.com/juju/juju/internal/testhelpers"
)

type recoverySuite struct {
	testhelpers.IsolationSuite
}

func TestRecoverySuite(t *testing.T) {
	tc.Run(t, &recoverySuite{})
}

func (s *recoverySuite) TestRecoverDqliteRunsStage(c *tc.C) {
	const bootstrapAddress = "10.0.0.1"
	addresses := network.NewMachineAddresses(
		[]string{bootstrapAddress}, network.WithScope(network.ScopeCloudLocal),
	).AsProviderAddresses()
	mgr := &testNodeManager{c: c}

	stageRan := false
	stage := func(ctx context.Context, dqlite *app.App) error {
		stageRan = true

		// The stage receives the open app and can create and migrate a
		// fresh model database.
		db, err := EnsureModelDatabase(ctx, dqlite,
			"00000000-0000-0000-0000-000000000001", loggertesting.WrapCheckLog(c))
		if err != nil {
			return err
		}
		defer func() { _ = db.Close() }()

		var name string
		return db.QueryRowContext(ctx,
			"SELECT name FROM sqlite_master WHERE name='change_log'").Scan(&name)
	}

	err := RecoverDqlite(
		c.Context(), mgr, addresses, tc.Must0(c, coremodel.NewUUID),
		loggertesting.WrapCheckLog(c), stage,
		func(ctx context.Context, controller, model database.TxnRunner) error {
			// Seed operations run after the stage.
			c.Check(stageRan, tc.IsTrue)
			return nil
		},
	)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(stageRan, tc.IsTrue)
}
