// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package restoreimport

import (
	"context"

	"github.com/juju/juju/core/database"
	ctrlv4_1_0 "github.com/juju/juju/domain/export/types/controller/v4_1_0"
	"github.com/juju/juju/domain/export/types/v4_1_0"
	controllerstate "github.com/juju/juju/domain/restoreimport/state/controller"
	modelstate "github.com/juju/juju/domain/restoreimport/state/model"
	"github.com/juju/juju/internal/errors"
)

// ControllerOverlay is the target-local controller overlay, carried by
// [controllerstate.ControllerOverlay].
type ControllerOverlay = controllerstate.ControllerOverlay

// ModelOverlay is the target-local model overlay, carried by
// [modelstate.ModelOverlay].
type ModelOverlay = modelstate.ModelOverlay

// ControllerImporter restores a controller database from a source export,
// overlaying the captured target-local facts.
type ControllerImporter struct {
	state *controllerstate.State
}

// NewControllerImporter returns an importer writing to the controller DB
// reachable through the given transaction-runner factory.
func NewControllerImporter(factory database.TxnRunnerFactory) *ControllerImporter {
	return &ControllerImporter{state: controllerstate.NewState(factory)}
}

// Import bulk-inserts the source controller logical rows and then applies the
// target-local overlay. Before importing it removes the temporary bootstrap
// controller-model row: the source controller model carries the same
// qualified name (name, qualifier) and would collide on the unique index.
// The temporary controller model's dqlite namespace database is deleted only
// after this phase has committed (by the restore orchestrator).
func (i *ControllerImporter) Import(ctx context.Context, payload *ctrlv4_1_0.ControllerExport, overlay ControllerOverlay, tempControllerModelUUID string) error {
	if payload == nil {
		return nil
	}
	if err := i.state.DeleteModel(ctx, tempControllerModelUUID); err != nil {
		return errors.Errorf("removing temporary controller model: %w", err)
	}
	if err := i.state.Import(ctx, payload); err != nil {
		return errors.Errorf("importing controller data: %w", err)
	}
	if err := i.state.ApplyOverlay(ctx, payload, overlay); err != nil {
		return errors.Errorf("applying controller overlay: %w", err)
	}
	return nil
}

// ModelImporter restores a model database from a source export, overlaying
// the captured target-local facts.
type ModelImporter struct {
	state *modelstate.State
}

// NewModelImporter returns an importer writing to the model DB reachable
// through the given transaction-runner factory.
func NewModelImporter(factory database.TxnRunnerFactory) *ModelImporter {
	return &ModelImporter{state: modelstate.NewState(factory)}
}

// Import bulk-inserts the source model rows and then applies the target-local
// overlay.
func (i *ModelImporter) Import(ctx context.Context, payload *v4_1_0.ModelExport, overlay ModelOverlay) error {
	if payload == nil {
		return nil
	}
	if err := i.state.Import(ctx, payload); err != nil {
		return errors.Errorf("importing model data: %w", err)
	}
	if err := i.state.ApplyOverlay(ctx, payload, overlay); err != nil {
		return errors.Errorf("applying model overlay: %w", err)
	}
	return nil
}
