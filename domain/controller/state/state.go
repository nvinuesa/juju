// Copyright 2024 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package state

import (
	"context"
	"fmt"

	"github.com/canonical/sqlair"
	"github.com/juju/errors"

	"github.com/juju/juju/core/database"
	"github.com/juju/juju/core/model"
	"github.com/juju/juju/domain"
)

// State represents a type for interacting with the underlying state.
type State struct {
	*domain.StateBase
}

// NewState returns a new State for interacting with the underlying state.
func NewState(factory database.TxnRunnerFactory) *State {
	return &State{
		StateBase: domain.NewStateBase(factory),
	}
}

// Controller returns the controller UUID and the controller model UUID.
func (st *State) Controller(ctx context.Context) (string, model.UUID, error) {
	db, err := st.DB()
	if err != nil {
		return "", "", errors.Trace(err)
	}

	var controller controller
	stmt, err := st.Prepare(`
SELECT &controller.*
FROM   controller
`, controller)
	if err != nil {
		return "", "", errors.Annotate(err, "preparing select controller model uuid statement")
	}
	err = db.Txn(ctx, func(ctx context.Context, tx *sqlair.TX) error {
		err := tx.Query(ctx, stmt).Get(&controller)
		if errors.Is(err, sqlair.ErrNoRows) {
			// This should never reasonably happen.
			return fmt.Errorf("internal error: controller model uuid not found")
		}
		return err
	})
	if err != nil {
		return "", "", errors.Annotate(err, "getting controller model uuid")
	}

	return controller.UUID, model.UUID(controller.ModelUUID), nil
}
