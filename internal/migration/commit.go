// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package migration

import (
	"context"

	coremodel "github.com/juju/juju/core/model"
	"github.com/juju/juju/core/semversion"
	modelerrors "github.com/juju/juju/domain/model/errors"
	"github.com/juju/juju/domain/modelmigration"
	modelmigrationerrors "github.com/juju/juju/domain/modelmigration/errors"
	"github.com/juju/juju/internal/errors"
	"github.com/juju/juju/internal/services"
)

// Migrating a model between two controllers is a commit across two controllers,
// and only the source can decide its outcome: it is the side that chooses
// between SUCCESS and ABORT. The target's job is to carry out that decision
// without ever guessing it.
//
// The source's phase machine makes the decision legible. Any error activating
// the model sends the source to ABORT, and it treats the target's cleanup as
// best effort. But once it durably records SUCCESS it can never reach ABORT
// again: it only rolls forward, retrying failures. So the target has exactly one
// reliable signal - the first message the source sends after recording SUCCESS,
// which is AdoptResources.
//
// The protocol therefore splits what used to be one operation:
//
//	PrepareActivation  all the fallible work, fully reversible. Called by the
//	                   source during VALIDATION, when it may still abort.
//	CommitActivation   the irreversible transition. Called by the source during
//	                   SUCCESS, so its arrival *is* the commit decision.
//
// This needs no new RPC and no new error code, so an unmodified 3.6 or 4.0
// source speaks it unchanged.

// PrepareActivation performs every fallible, reversible step of activating an
// imported model, and nothing else.
//
// It is Juju 3.6's Activate minus its final act of releasing the model. Each
// step is idempotent, and every failure leaves the model exactly as it was:
// claim still importing, gates still closed, workers still parked. That is the
// point - the source treats any failure here, or a reply it never receives, as
// a reason to abort, so nothing that happens here may prevent that abort from
// succeeding.
func PrepareActivation(
	ctx context.Context, domainServices services.DomainServices, args ActivateModelArgs,
) error {
	modelUUID := args.ModelUUID
	claim, err := domainServices.ModelMigration().GetImportClaim(ctx, modelUUID)
	switch {
	case errors.Is(err, modelmigrationerrors.ErrImportNotFound):
		// No claim: either this model was never imported, or it was already
		// committed and released. Only the latter is a success, and only if the
		// model really is finished - otherwise report the model is not importing
		// rather than letting a caller believe preparation happened.
		if committed, err := committedPredicates(ctx, domainServices, modelUUID); err != nil {
			return errors.Capture(err)
		} else if committed {
			return nil
		}
		return errors.Errorf("model %q is not importing", modelUUID)
	case err != nil:
		return errors.Errorf("reading import claim for model %q: %w", modelUUID, err)
	}

	switch claim.Phase {
	case modelmigration.ImportPhaseAborting:
		// Cleanup has started. The source will abort anyway, so refusing here
		// costs nothing and avoids writing into a model being torn down.
		return errors.Errorf("model %q: %w", modelUUID, modelmigrationerrors.ErrActivationAborting)
	case modelmigration.ImportPhaseActivating:
		// The commit has already been received, so there is nothing left to
		// prepare. Reporting success is safer than failing: a stale caller that
		// failed here would drive an abort that the target must then refuse.
		return nil
	case modelmigration.ImportPhaseImporting:
	default:
		return errors.Errorf("model %q: unexpected import claim phase %q", modelUUID, claim.Phase)
	}

	if err := reconcileOffererControllers(ctx, domainServices, modelUUID, true, args); err != nil {
		return errors.Errorf(
			"reconciling offerer controller UUIDs for model %q: %w", modelUUID, err)
	}

	if err := reconcileModelAgentVersion(ctx, domainServices, modelUUID.String()); err != nil {
		return errors.Errorf(
			"reconciling model agent version during activation of model %q: %w", modelUUID, err)
	}

	// Re-check the claim last. Preparation writes shared controller rows using
	// compare-or-insert, so an abort racing it has nothing to undo, but a caller
	// that is told preparation succeeded should not then be surprised by a
	// refused commit.
	if err := domainServices.ModelMigration().AssertImporting(ctx, modelUUID); err != nil {
		return errors.Errorf("model %q import claim changed during activation: %w", modelUUID, err)
	}
	return nil
}

// CommitActivation performs the irreversible half of activation: it records
// that the source committed, releases the model, and then adopts its cloud
// resources.
//
// It is driven by the AdoptResources call, which a source sends only after
// durably recording SUCCESS. Receipt is therefore proof that the source can
// never abort this migration, which is what makes the transition safe to treat
// as a point of no return.
//
// The order matches Juju 3.6, which released the model in Activate and adopted
// resources afterwards. Adoption failing does not undo the release: the model is
// already this controller's, and the source retries AdoptResources until it
// succeeds, exactly as before.
func CommitActivation(
	ctx context.Context,
	domainServices services.DomainServices,
	modelUUID coremodel.UUID,
	sourceControllerVersion semversion.Number,
) error {
	release, err := commitClaim(ctx, domainServices, modelUUID, sourceControllerVersion)
	if err != nil {
		return errors.Capture(err)
	}
	if release {
		if err := releaseModel(ctx, domainServices, modelUUID); err != nil {
			return errors.Capture(err)
		}
	}

	// Adopt last, and let its error surface: the source retries this call, and
	// a retry finds no claim, confirms the model is already released, and
	// re-runs adoption alone.
	return domainServices.ModelMigration().AdoptResources(ctx, sourceControllerVersion)
}

// commitClaim records the source's commit on the claim, reporting whether the
// model still needs releasing.
func commitClaim(
	ctx context.Context,
	domainServices services.DomainServices,
	modelUUID coremodel.UUID,
	sourceControllerVersion semversion.Number,
) (bool, error) {
	claim, err := domainServices.ModelMigration().GetImportClaim(ctx, modelUUID)
	switch {
	case errors.Is(err, modelmigrationerrors.ErrImportNotFound):
		// The claim is gone, so either this commit already completed and the
		// reply was lost, or the model was never imported here. Only the former
		// may proceed, and only to re-run adoption.
		committed, err := committedPredicates(ctx, domainServices, modelUUID)
		if err != nil {
			return false, errors.Capture(err)
		}
		if !committed {
			return false, errors.Errorf(
				"model %q is not importing or activating", modelUUID)
		}
		return false, nil
	case err != nil:
		return false, errors.Errorf("reading import claim for model %q: %w", modelUUID, err)
	}

	switch claim.Phase {
	case modelmigration.ImportPhaseAborting:
		// Unreachable from a correct source: a migration that reached SUCCESS
		// can never have driven an abort. Refuse loudly rather than tear down a
		// model the source believes it handed over.
		return false, errors.Errorf(
			"model %q: cannot commit activation, import is aborting", modelUUID)
	case modelmigration.ImportPhaseActivating:
		// An interrupted commit. Everything below is idempotent, so resume.
		return true, nil
	case modelmigration.ImportPhaseImporting:
		// The commit record itself, written with the source's version so that a
		// commit interrupted here can be finished without the source.
		if err := domainServices.ModelMigration().SetImportPhaseActivating(
			ctx, modelUUID, sourceControllerVersion.String(),
		); err != nil {
			return false, errors.Errorf(
				"recording activation commit for model %q: %w", modelUUID, err)
		}
		return true, nil
	default:
		return false, errors.Errorf(
			"model %q: unexpected import claim phase %q", modelUUID, claim.Phase)
	}
}

// releaseModel makes a committed model usable and gives up the claim. Every
// step is idempotent, so a commit interrupted part-way is resumed by repeating
// all of them.
//
// Claim deletion is last and is what actually releases the model: it is the
// signal the migration flag watches, so the model's workers start at that
// moment and not before.
func releaseModel(
	ctx context.Context, domainServices services.DomainServices, modelUUID coremodel.UUID,
) error {
	if err := domainServices.ModelMigration().DeleteModelImportingStatus(ctx); err != nil {
		return errors.Errorf("clearing import gate for model %q: %w", modelUUID, err)
	}

	// model.activated is the generic "model creation is complete" flag every
	// model carries, distinct from the migration claim. The import sets it so
	// agents can reach the model during validation, so this is normally a no-op.
	if err := domainServices.Model().ActivateModel(ctx, modelUUID); err != nil &&
		!errors.Is(err, modelerrors.AlreadyActivated) {
		return errors.Errorf("activating model %q: %w", modelUUID, err)
	}

	if err := domainServices.ModelMigration().DeleteActivatedImport(ctx, modelUUID); err != nil {
		return errors.Errorf("releasing import claim for model %q: %w", modelUUID, err)
	}
	return nil
}

// committedPredicates reports whether a model with no import claim shows every
// sign of having been released by a completed commit.
//
// A missing claim is ambiguous on its own - it equally describes a model that
// was never imported, or one whose abort finished - so callers that treat it as
// success must confirm it here rather than assume.
func committedPredicates(
	ctx context.Context, domainServices services.DomainServices, modelUUID coremodel.UUID,
) (bool, error) {
	// CheckModelExists is false for a model that is absent *or* not yet
	// activated, which is exactly the distinction wanted here.
	exists, err := domainServices.Model().CheckModelExists(ctx, modelUUID)
	if err != nil {
		return false, errors.Errorf("checking model %q exists: %w", modelUUID, err)
	}
	if !exists {
		return false, nil
	}

	gated, err := domainServices.ModelMigration().IsModelImporting(ctx)
	if err != nil {
		return false, errors.Errorf("checking import gate for model %q: %w", modelUUID, err)
	}
	return !gated, nil
}
