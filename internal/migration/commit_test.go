// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package migration_test

import (
	"context"
	"database/sql"

	"github.com/juju/clock"
	"github.com/juju/tc"

	coremodel "github.com/juju/juju/core/model"
	"github.com/juju/juju/core/semversion"
	"github.com/juju/juju/domain/modelmigration"
	modelmigrationerrors "github.com/juju/juju/domain/modelmigration/errors"
	migrationclaimstate "github.com/juju/juju/domain/modelmigration/state/controller"
	"github.com/juju/juju/internal/migration"
	"github.com/juju/juju/internal/services"
)

// sourceVersion stands in for the source controller's version, which the
// commit records so an interrupted commit can be finished without the source.
var sourceVersion = semversion.MustParse("4.0.12")

func (s *controllerImportSuite) domainServices(
	c *tc.C, deps migration.Deps, modelUUID coremodel.UUID,
) services.DomainServices {
	svc, err := activationDomainServicesGetter{deps: deps}.ServicesForModel(c.Context(), modelUUID)
	c.Assert(err, tc.ErrorIsNil)
	return svc
}

func (s *controllerImportSuite) claimPhase(
	c *tc.C, modelUUID coremodel.UUID,
) modelmigration.ImportPhase {
	st := migrationclaimstate.New(s.TxnRunnerFactory(), clock.WallClock)
	claim, err := st.GetImportClaim(c.Context(), modelUUID.String())
	c.Assert(err, tc.ErrorIsNil)
	return claim.Phase
}

func (s *controllerImportSuite) claimSourceVersion(c *tc.C, modelUUID coremodel.UUID) *string {
	var version *string
	err := s.TxnRunner().StdTxn(c.Context(), func(ctx context.Context, tx *sql.Tx) error {
		return tx.QueryRowContext(ctx,
			"SELECT source_controller_version FROM model_migration_import WHERE model_uuid = ?",
			modelUUID.String()).Scan(&version)
	})
	c.Assert(err, tc.ErrorIsNil)
	return version
}

// TestPrepareActivationLeavesModelAbortable is the central guarantee of the
// protocol: preparing a model must not commit anything.
//
// The source calls this during VALIDATION, where any error - or a reply it
// never receives - sends it to ABORT. If preparation moved the claim, cleared
// the gate or released the model, that abort would arrive at a target that had
// already begun handing the model over, and the model would end up live on both
// controllers.
func (s *controllerImportSuite) TestPrepareActivationLeavesModelAbortable(c *tc.C) {
	modelUUID, deps := s.importForActivation(c, "1.0.0")

	err := migration.PrepareActivation(
		c.Context(), s.domainServices(c, deps, modelUUID),
		migration.ActivateModelArgs{ModelUUID: modelUUID})
	c.Assert(err, tc.ErrorIsNil)

	c.Check(s.claimPhase(c, modelUUID), tc.Equals, modelmigration.ImportPhaseImporting)
	c.Check(s.modelGateExists(c, modelUUID), tc.IsTrue)
	// Nothing recorded a commit, so the claim carries no source version.
	c.Check(s.claimSourceVersion(c, modelUUID), tc.IsNil)
}

// TestPrepareActivationIsIdempotent asserts preparation can be repeated, which
// a source restarting in VALIDATION will do.
func (s *controllerImportSuite) TestPrepareActivationIsIdempotent(c *tc.C) {
	modelUUID, deps := s.importForActivation(c, "1.0.0")
	svc := s.domainServices(c, deps, modelUUID)
	args := migration.ActivateModelArgs{ModelUUID: modelUUID}

	c.Assert(migration.PrepareActivation(c.Context(), svc, args), tc.ErrorIsNil)
	c.Assert(migration.PrepareActivation(c.Context(), svc, args), tc.ErrorIsNil)

	c.Check(s.claimPhase(c, modelUUID), tc.Equals, modelmigration.ImportPhaseImporting)
}

// TestPrepareActivationRefusesAbortingClaim asserts preparation stops once
// cleanup has started, rather than writing into a model being torn down.
func (s *controllerImportSuite) TestPrepareActivationRefusesAbortingClaim(c *tc.C) {
	modelUUID, deps := s.importForActivation(c, "1.0.0")
	s.setClaimAborting(c, modelUUID)

	err := migration.PrepareActivation(
		c.Context(), s.domainServices(c, deps, modelUUID),
		migration.ActivateModelArgs{ModelUUID: modelUUID})
	c.Check(err, tc.ErrorIs, modelmigrationerrors.ErrActivationAborting)
}

// TestCommitActivationReleasesModel asserts the commit does what preparation
// deliberately did not: record the source's decision, clear the gate, and give
// up the claim. Claim deletion is what actually releases the model, so it must
// be gone when this returns.
func (s *controllerImportSuite) TestCommitActivationReleasesModel(c *tc.C) {
	modelUUID, deps := s.importForActivation(c, "1.0.0")

	err := migration.CommitActivation(
		c.Context(), s.domainServices(c, deps, modelUUID), modelUUID, sourceVersion)
	c.Assert(err, tc.ErrorIsNil)

	c.Check(s.modelActivated(c, modelUUID), tc.IsTrue)
	c.Check(s.modelGateExists(c, modelUUID), tc.IsFalse)

	st := migrationclaimstate.New(s.TxnRunnerFactory(), clock.WallClock)
	_, err = st.GetImportClaim(c.Context(), modelUUID.String())
	c.Check(err, tc.ErrorIs, modelmigrationerrors.ErrImportNotFound)
}

// TestCommitActivationResumesFromActivating asserts a commit interrupted after
// its transition is completed by a retry. The source retries AdoptResources
// until it succeeds, so this is the ordinary recovery path.
func (s *controllerImportSuite) TestCommitActivationResumesFromActivating(c *tc.C) {
	modelUUID, deps := s.importForActivation(c, "1.0.0")

	st := migrationclaimstate.New(s.TxnRunnerFactory(), clock.WallClock)
	c.Assert(st.SetImportPhaseActivating(
		c.Context(), modelUUID.String(), sourceVersion.String()), tc.ErrorIsNil)

	err := migration.CommitActivation(
		c.Context(), s.domainServices(c, deps, modelUUID), modelUUID, sourceVersion)
	c.Assert(err, tc.ErrorIsNil)

	_, err = st.GetImportClaim(c.Context(), modelUUID.String())
	c.Check(err, tc.ErrorIs, modelmigrationerrors.ErrImportNotFound)
}

// TestCommitActivationIsIdempotentAfterRelease asserts a retry that arrives
// after the claim is already gone succeeds, rather than reporting the model is
// not importing. The source cannot tell a lost reply from a failure, so it will
// send this call again.
func (s *controllerImportSuite) TestCommitActivationIsIdempotentAfterRelease(c *tc.C) {
	modelUUID, deps := s.importForActivation(c, "1.0.0")
	svc := s.domainServices(c, deps, modelUUID)

	c.Assert(migration.CommitActivation(c.Context(), svc, modelUUID, sourceVersion), tc.ErrorIsNil)
	c.Assert(migration.CommitActivation(c.Context(), svc, modelUUID, sourceVersion), tc.ErrorIsNil)

	c.Check(s.modelActivated(c, modelUUID), tc.IsTrue)
}

// TestCommitActivationRefusesAbortingClaim asserts a commit is refused while
// the model is being torn down. A correct source cannot produce this - a
// migration that reached SUCCESS never drove an abort - so it fails loudly
// rather than resurrecting a model whose cleanup is under way.
func (s *controllerImportSuite) TestCommitActivationRefusesAbortingClaim(c *tc.C) {
	modelUUID, deps := s.importForActivation(c, "1.0.0")
	s.setClaimAborting(c, modelUUID)

	err := migration.CommitActivation(
		c.Context(), s.domainServices(c, deps, modelUUID), modelUUID, sourceVersion)
	c.Check(err, tc.ErrorMatches, ".*cannot commit activation, import is aborting.*")
}

// setClaimAborting moves the model's import claim to the aborting phase,
// standing in for a concurrent abort.
func (s *controllerImportSuite) setClaimAborting(c *tc.C, modelUUID coremodel.UUID) {
	err := s.TxnRunner().StdTxn(c.Context(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
UPDATE model_migration_import
SET    phase_type_id = (
           SELECT id FROM model_migration_import_phase_type WHERE type = 'aborting')
WHERE  model_uuid = ?`, modelUUID.String())
		return err
	})
	c.Assert(err, tc.ErrorIsNil)
}
