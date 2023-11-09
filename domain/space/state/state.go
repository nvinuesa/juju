// Copyright 2023 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package state

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/juju/errors"
	"github.com/juju/names/v4"
	"github.com/juju/utils/v3"

	coreDB "github.com/juju/juju/core/database"
	"github.com/juju/juju/core/network"
	"github.com/juju/juju/domain"
)

// State represents a type for interacting with the underlying state.
type State struct {
	*domain.StateBase
}

// NewState returns a new State for interacting with the underlying state.
func NewState(factory coreDB.TxnRunnerFactory) *State {
	return &State{
		StateBase: domain.NewStateBase(factory),
	}
}

// Add creates and returns a new space.
func (st *State) AddSpace(
	ctx context.Context,
	uuid utils.UUID,
	name string,
	providerId network.Id,
	subnetIDs []string,
	isPublic bool,
) error {
	if !names.IsValidSpace(name) {
		return errors.NewNotValid(nil, fmt.Sprintf("invalid space name '%s'", name))
	}

	db, err := st.DB()
	if err != nil {
		return errors.Trace(err)
	}

	insertSpaceStmt := `
INSERT INTO space (uuid, name, is_public)
VALUES (?, ?, ?)`
	insertProviderStmt := `
INSERT INTO provider_space (provider_id, space_uuid)
VALUES (?, ?)`
	updateSubnetStmt := `
UPDATE subnet
SET space_uuid = ?
WHERE uuid = ?`
	err = db.StdTxn(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, insertSpaceStmt, uuid.String(), name, isPublic); err != nil {
			return errors.Trace(err)
		}
		if _, err := tx.ExecContext(ctx, insertProviderStmt, providerId, uuid.String()); err != nil {
			return errors.Trace(err)
		}
		for _, subnetID := range subnetIDs {
			if _, err := tx.ExecContext(ctx, updateSubnetStmt, uuid.String(), subnetID); err != nil {
				return errors.Trace(err)
			}
		}
		return nil
	})
	return errors.Trace(err)
}
