// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package migrationstatus

import (
	"context"

	"github.com/juju/juju/domain/modelmigration"
)

// ModelMigrationService provides access to the migration status. This
// interface is deliberately narrow: it exposes only the credential-free
// [MigrationStatusForModel] read path. The facade struct holds this interface
// and cannot call InitiateMigration, SetMigrationPhase,
// SetMigrationStatusMessage, or the credential-carrying Migration() method.
type ModelMigrationService interface {
	// MigrationStatusForModel returns the credential-free status of the
	// latest migration for the given model UUID. The model does not need
	// to still exist in the model table — the migration export history
	// survives REAP. If no migration has ever been attempted for the
	// model, a MigrationStatusInfo with phase NONE is returned.
	MigrationStatusForModel(ctx context.Context, modelUUID string) (modelmigration.MigrationStatusInfo, error)
}
