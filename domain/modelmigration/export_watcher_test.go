// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package modelmigration_test

import (
	"context"
	"testing"

	"github.com/juju/clock"
	"github.com/juju/tc"

	"github.com/juju/juju/core/changestream"
	coredatabase "github.com/juju/juju/core/database"
	"github.com/juju/juju/core/migration"
	"github.com/juju/juju/core/providertracker"
	"github.com/juju/juju/core/watcher/watchertest"
	"github.com/juju/juju/domain"
	"github.com/juju/juju/domain/modelmigration"
	"github.com/juju/juju/domain/modelmigration/service"
	migrationstatecontroller "github.com/juju/juju/domain/modelmigration/state/controller"
	changestreamtesting "github.com/juju/juju/internal/changestream/testing"
	loggertesting "github.com/juju/juju/internal/logger/testing"
	"github.com/juju/juju/internal/uuid"
)

// exportWatcherSuite exercises the source-side export watchers end-to-end
// against the controller database, verifying that the export and minion-sync
// changestream triggers fire and reach the service watchers.
type exportWatcherSuite struct {
	changestreamtesting.ControllerSuite

	modelUUID string
}

func TestExportWatcherSuite(t *testing.T) {
	tc.Run(t, &exportWatcherSuite{})
}

func (s *exportWatcherSuite) SetUpTest(c *tc.C) {
	s.ControllerSuite.SetUpTest(c)
	s.modelUUID = uuid.MustNewUUID().String()
}

// TestWatchForMigration asserts the existence watcher fires when an export
// migration starts and ends, but NOT on intermediate phase transitions.
func (s *exportWatcherSuite) TestWatchForMigration(c *tc.C) {
	st := migrationstatecontroller.New(s.controllerDBFactory(), clock.WallClock)
	factory := changestream.NewWatchableDBFactoryForNamespace(s.GetWatchableDB, "model_migration_export")
	svc := s.setupService(c, st, factory)

	spec := s.newSpec()

	s.AssertChangeStreamIdle(c)
	w, err := svc.WatchForMigration(c.Context())
	c.Assert(err, tc.ErrorIsNil)

	harness := watchertest.NewHarness(s, watchertest.NewWatcherC(c, w))

	harness.AddTest(c, func(c *tc.C) {}, func(w watchertest.WatcherC[struct{}]) {
		w.AssertNoChange()
	})

	// Recording the export (migration start) fires the watcher.
	harness.AddTest(c, func(c *tc.C) {
		err := st.InsertExport(c.Context(), spec)
		c.Assert(err, tc.ErrorIsNil)
	}, func(w watchertest.WatcherC[struct{}]) {
		w.AssertChange()
	})

	// An intermediate phase change must NOT fire the existence watcher.
	harness.AddTest(c, func(c *tc.C) {
		err := st.SetPhase(c.Context(), spec.MigrationUUID, migration.IMPORT)
		c.Assert(err, tc.ErrorIsNil)
	}, func(w watchertest.WatcherC[struct{}]) {
		w.AssertNoChange()
	})

	// Ending the export (end_time set) fires the watcher.
	harness.AddTest(c, func(c *tc.C) {
		err := st.MarkExportEnded(c.Context(), spec.MigrationUUID, migration.ABORTDONE)
		c.Assert(err, tc.ErrorIsNil)
	}, func(w watchertest.WatcherC[struct{}]) {
		w.AssertChange()
	})

	harness.Run(c, struct{}{})
}

// TestWatchMigrationPhase asserts the phase watcher fires on the initial phase
// (recorded at export start) and on each subsequent phase transition.
func (s *exportWatcherSuite) TestWatchMigrationPhase(c *tc.C) {
	st := migrationstatecontroller.New(s.controllerDBFactory(), clock.WallClock)
	factory := changestream.NewWatchableDBFactoryForNamespace(s.GetWatchableDB, "model_migration_export_phase")
	svc := s.setupService(c, st, factory)

	spec := s.newSpec()

	s.AssertChangeStreamIdle(c)
	w, err := svc.WatchMigrationPhase(c.Context())
	c.Assert(err, tc.ErrorIsNil)

	harness := watchertest.NewHarness(s, watchertest.NewWatcherC(c, w))

	harness.AddTest(c, func(c *tc.C) {}, func(w watchertest.WatcherC[struct{}]) {
		w.AssertNoChange()
	})

	// The QUIESCE phase recorded at export start fires the watcher.
	harness.AddTest(c, func(c *tc.C) {
		err := st.InsertExport(c.Context(), spec)
		c.Assert(err, tc.ErrorIsNil)
	}, func(w watchertest.WatcherC[struct{}]) {
		w.AssertChange()
	})

	// A subsequent phase change fires the watcher.
	harness.AddTest(c, func(c *tc.C) {
		err := st.SetPhase(c.Context(), spec.MigrationUUID, migration.IMPORT)
		c.Assert(err, tc.ErrorIsNil)
	}, func(w watchertest.WatcherC[struct{}]) {
		w.AssertChange()
	})

	harness.Run(c, struct{}{})
}

// TestWatchMinionReports asserts the minion watcher fires when a minion report
// is recorded for the model's active migration.
func (s *exportWatcherSuite) TestWatchMinionReports(c *tc.C) {
	st := migrationstatecontroller.New(s.controllerDBFactory(), clock.WallClock)
	factory := changestream.NewWatchableDBFactoryForNamespace(s.GetWatchableDB, "model_migration_export_minion_sync")
	svc := s.setupService(c, st, factory)

	// The minion watcher resolves the active migration UUID, so the export must
	// already exist.
	spec := s.newSpec()
	err := st.InsertExport(c.Context(), spec)
	c.Assert(err, tc.ErrorIsNil)

	s.AssertChangeStreamIdle(c)
	w, err := svc.WatchMinionReports(c.Context())
	c.Assert(err, tc.ErrorIsNil)

	harness := watchertest.NewHarness(s, watchertest.NewWatcherC(c, w))

	harness.AddTest(c, func(c *tc.C) {}, func(w watchertest.WatcherC[struct{}]) {
		w.AssertNoChange()
	})

	harness.AddTest(c, func(c *tc.C) {
		err := st.InsertMinionReport(c.Context(), spec.MigrationUUID, migration.QUIESCE, "machine-0", true)
		c.Assert(err, tc.ErrorIsNil)
	}, func(w watchertest.WatcherC[struct{}]) {
		w.AssertChange()
	})

	harness.Run(c, struct{}{})
}

func (s *exportWatcherSuite) newSpec() modelmigration.MigrationSpec {
	return modelmigration.MigrationSpec{
		MigrationUUID: uuid.MustNewUUID().String(),
		ModelUUID:     s.modelUUID,
		Target: migration.TargetInfo{
			ControllerUUID: uuid.MustNewUUID().String(),
			Addrs:          []string{"10.0.0.1:17070"},
			CACert:         "ca-cert",
			User:           "admin",
			Password:       "secret",
		},
	}
}

func (s *exportWatcherSuite) controllerDBFactory() coredatabase.TxnRunnerFactory {
	return func(ctx context.Context) (coredatabase.TxnRunner, error) {
		return s.ControllerTxnRunner(), nil
	}
}

func (s *exportWatcherSuite) setupService(
	c *tc.C, st service.ControllerState, factory domain.WatchableDBFactory,
) *service.Service {
	noopInstanceGetter := func(context.Context) (service.InstanceProvider, error) {
		return nil, nil
	}
	noopResourceGetter := func(context.Context) (service.ResourceProvider, error) {
		return nil, nil
	}

	return service.NewService(
		st,
		nil,
		s.modelUUID,
		domain.NewWatcherFactory(factory, loggertesting.WrapCheckLog(c)),
		providertracker.ProviderGetter[service.InstanceProvider](noopInstanceGetter),
		providertracker.ProviderGetter[service.ResourceProvider](noopResourceGetter),
	)
}
