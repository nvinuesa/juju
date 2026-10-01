// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package environs

import (
	"context"
)

// RestoreModel identifies one archived model for restore substrate
// checks.
type RestoreModel struct {
	// Name is the model's name; on Kubernetes it is also the model's
	// namespace name.
	Name string

	// UUID is the model's logical identity, preserved by restore.
	UUID string
}

// RestoreSubstrateParams describes the surviving substrate a provider
// must verify before a restore bootstrap provisions anything.
type RestoreSubstrateParams struct {
	// ControllerUUID is the archived source controller's identity.
	ControllerUUID string

	// ControllerName is the archived source controller's name; on
	// Kubernetes the controller namespace derives from it.
	ControllerName string

	// ControllerModelUUID identifies the controller model; its namespace
	// is the controller namespace, checked separately.
	ControllerModelUUID string

	// Models is the archived model inventory, including the controller
	// model.
	Models []RestoreModel
}

// RestoreSubstrateChecker is implemented by providers that can verify a
// restore target's surviving substrate before provisioning. The check is
// read-only; any failure aborts bootstrap before anything is created.
type RestoreSubstrateChecker interface {
	CheckRestoreSubstrate(ctx context.Context, params RestoreSubstrateParams) error
}
