// Copyright 2023 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package recovery

import (
	"context"

	"github.com/canonical/sqlair"

	"github.com/juju/juju/core/database"
	"github.com/juju/juju/core/user"
	usererrors "github.com/juju/juju/domain/access/errors"
	"github.com/juju/juju/domain/access/state"
	"github.com/juju/juju/internal/auth"
	"github.com/juju/juju/internal/errors"
)

// SetUserPassword rebinds the recovery client password of an existing user.
// Other archived user attributes and permissions are preserved.
func SetUserPassword(ctx context.Context, db database.TxnRunner, name user.Name, password auth.Password) error {
	defer password.Destroy()
	if name.IsZero() {
		return errors.Errorf("%q: %w", name, usererrors.UserNameNotValid)
	}
	salt, err := auth.NewSalt()
	if err != nil {
		return errors.Capture(err)
	}
	hash, err := auth.HashPassword(password, salt)
	if err != nil {
		return errors.Capture(err)
	}
	return errors.Capture(db.Txn(ctx, func(ctx context.Context, tx *sqlair.TX) error {
		return state.SetPasswordHashForRecovery(ctx, tx, name, hash, salt)
	}))
}
