// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package modelmigration

import (
	"context"

	"github.com/juju/clock"
	"github.com/juju/description/v12"

	"github.com/juju/juju/core/logger"
	coremodel "github.com/juju/juju/core/model"
	"github.com/juju/juju/core/modelmigration"
	"github.com/juju/juju/domain/modelmigration/state/controller"
	"github.com/juju/juju/internal/errors"
)

// Coordinator is the interface that is used to add operations to a migration.
type Coordinator interface {
	// Add adds the given operation to the migration.
	Add(modelmigration.Operation)
}

// ImportOfferLedgerState records the offers an import granted permissions on.
type ImportOfferLedgerState interface {
	// RecordImportedOffers records offer UUIDs against the model's import
	// claim.
	RecordImportedOffers(ctx context.Context, modelUUID string, offerUUIDs []string) error
}

// RegisterImportOfferLedger registers the operation that records this import's
// offers against its import claim.
//
// It must be registered *after* the offer access import, so that the ledger
// only ever names offers whose permission rows were actually written.
func RegisterImportOfferLedger(coordinator Coordinator, clock clock.Clock, logger logger.Logger) {
	coordinator.Add(&importOfferLedgerOperation{
		clock:  clock,
		logger: logger,
	})
}

// importOfferLedgerOperation records the UUIDs of offers this import granted
// permissions on, so that aborting the import can find those permission rows
// again.
//
// Offer permissions are granted on the *offer* UUID, not the model UUID, and
// nothing in the controller database links an offer back to its model: the
// offers themselves live in the model database, which an abort drops. Without
// this ledger an aborted import leaves its offer-permission rows behind with no
// way to find them.
//
// The offer access import's own Rollback covers failure *during* the import.
// This covers the other case: an import that returned successfully and is
// aborted later, from the source's VALIDATION phase, when no rollback runs.
type importOfferLedgerOperation struct {
	modelmigration.BaseOperation

	state     ImportOfferLedgerState
	modelUUID coremodel.UUID

	clock  clock.Clock
	logger logger.Logger
}

// Name returns the name of this operation.
func (i *importOfferLedgerOperation) Name() string {
	return "record imported offers against the import claim"
}

// Setup implements Operation.
func (i *importOfferLedgerOperation) Setup(scope modelmigration.Scope) error {
	i.state = controller.New(scope.ControllerDB(), i.clock)
	i.modelUUID = scope.ModelUUID()
	return nil
}

// Execute records every offer in the model against the import claim.
func (i *importOfferLedgerOperation) Execute(ctx context.Context, model description.Model) error {
	var offerUUIDs []string
	for _, app := range model.Applications() {
		for _, offer := range app.Offers() {
			offerUUIDs = append(offerUUIDs, offer.OfferUUID())
		}
	}
	if len(offerUUIDs) == 0 {
		return nil
	}
	if err := i.state.RecordImportedOffers(ctx, i.modelUUID.String(), offerUUIDs); err != nil {
		return errors.Errorf(
			"recording imported offers for model %q: %w", i.modelUUID, err)
	}
	return nil
}

// Rollback is a no-op. The ledger rows hang off the import claim and are
// removed with it, either by abort finalization or by the model teardown that
// follows a legacy abort. Deleting them here would strand the offer-permission
// rows they exist to point at.
func (i *importOfferLedgerOperation) Rollback(context.Context, description.Model) error {
	return nil
}
