// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package migration

import (
	"context"
	"testing"

	"github.com/juju/clock"
	"github.com/juju/tc"

	coremodel "github.com/juju/juju/core/model"
	migrationclaimservice "github.com/juju/juju/domain/modelmigration/service"
	migrationclaimstate "github.com/juju/juju/domain/modelmigration/state/controller"
	schematesting "github.com/juju/juju/domain/schema/testing"
	loggertesting "github.com/juju/juju/internal/logger/testing"
	"github.com/juju/juju/internal/uuid"
)

// importFenceSuite exercises the fence [importCoordinator.Import] applies
// between operations. It is a white-box test because the fence is a property of
// the sequencing itself, not of any single operation, and because the racing
// abort it guards against cannot be staged through the exported entry point:
// re-driving a whole import is refused by the claim-creation step long before
// the fence is reached.
type importFenceSuite struct {
	schematesting.ControllerSuite
}

func TestImportFenceSuite(t *testing.T) {
	tc.Run(t, &importFenceSuite{})
}

// fenceOp is a stub import operation that records that it ran and optionally
// runs a hook, standing in for the real controller-DB write operations.
type fenceOp struct {
	name     string
	ran      *[]string
	claimID  string
	onExec   func(ctx context.Context) error
	setClaim bool
}

func (o *fenceOp) Name() string { return o.name }

func (o *fenceOp) Execute(ctx context.Context, st *importState) error {
	*o.ran = append(*o.ran, o.name)
	if o.setClaim {
		st.claimUUID = o.claimID
	}
	if o.onExec != nil {
		return o.onExec(ctx)
	}
	return nil
}

func (o *fenceOp) RemoveOnAbort(context.Context) error { return nil }

// abortClaim moves the model's import claim to the aborting phase, standing in
// for a concurrent abort. It is done in SQL because the abort orchestration
// itself is not what is under test here.
func (s *importFenceSuite) abortClaim(c *tc.C, modelUUID coremodel.UUID) error {
	_, err := s.DB().ExecContext(c.Context(),
		`UPDATE model_migration_import
		 SET phase_type_id = (
		     SELECT id FROM model_migration_import_phase_type WHERE type = 'aborting')
		 WHERE model_uuid = ?`, modelUUID.String())
	return err
}

// TestImportStopsWhenClaimLeavesImporting asserts that once the claim stops
// being importing, the sequence refuses to run any further operation.
//
// The claim is the model's ownership record. If an abort takes it while an
// import is mid-sequence, every write after that point lands behind the abort's
// back: the abort compensates the rows it knows about, and anything written
// afterwards is left with nothing to remove it.
func (s *importFenceSuite) TestImportStopsWhenClaimLeavesImporting(c *tc.C) {
	st := migrationclaimstate.New(s.TxnRunnerFactory(), clock.WallClock)
	claimSvc := migrationclaimservice.NewImportService(st, loggertesting.WrapCheckLog(c))

	modelUUID := tc.Must(c, coremodel.NewUUID)
	claimUUID := uuid.MustNewUUID().String()
	_, err := st.BeginImport(
		c.Context(), modelUUID.String(), claimUUID, uuid.MustNewUUID().String())
	c.Assert(err, tc.ErrorIsNil)

	var ran []string
	coord := &importCoordinator{
		claim:     claimSvc,
		modelUUID: modelUUID,
		ops: []controllerImportOp{
			// Stands in for opBeginImport: publishes the claim UUID, which is
			// what arms the fence for everything after it.
			&fenceOp{name: "begin", ran: &ran, claimID: claimUUID, setClaim: true},
			// An abort wins the claim while this operation is running.
			&fenceOp{name: "first-write", ran: &ran, onExec: func(ctx context.Context) error {
				return s.abortClaim(c, modelUUID)
			}},
			&fenceOp{name: "second-write", ran: &ran},
		},
	}

	err = coord.Import(c.Context())
	c.Assert(err, tc.NotNil)
	c.Check(err.Error(), tc.Contains, "second-write")

	// The operation that raced the abort still committed - the fence bounds the
	// window, it does not eliminate it - but nothing after it ran.
	c.Check(ran, tc.DeepEquals, []string{"begin", "first-write"})
}

// TestImportRunsEveryOpWhileImporting asserts the fence is not simply refusing
// everything: with the claim untouched, the whole sequence runs.
func (s *importFenceSuite) TestImportRunsEveryOpWhileImporting(c *tc.C) {
	st := migrationclaimstate.New(s.TxnRunnerFactory(), clock.WallClock)
	claimSvc := migrationclaimservice.NewImportService(st, loggertesting.WrapCheckLog(c))

	modelUUID := tc.Must(c, coremodel.NewUUID)
	claimUUID := uuid.MustNewUUID().String()
	_, err := st.BeginImport(
		c.Context(), modelUUID.String(), claimUUID, uuid.MustNewUUID().String())
	c.Assert(err, tc.ErrorIsNil)

	var ran []string
	coord := &importCoordinator{
		claim:     claimSvc,
		modelUUID: modelUUID,
		ops: []controllerImportOp{
			&fenceOp{name: "begin", ran: &ran, claimID: claimUUID, setClaim: true},
			&fenceOp{name: "first-write", ran: &ran},
			&fenceOp{name: "second-write", ran: &ran},
		},
	}

	c.Assert(coord.Import(c.Context()), tc.ErrorIsNil)
	c.Check(ran, tc.DeepEquals, []string{"begin", "first-write", "second-write"})
}
