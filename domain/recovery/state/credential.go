// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package state

import (
	"context"
	"database/sql"

	"github.com/canonical/sqlair"

	"github.com/juju/juju/internal/errors"
)

// PatchControllerCredential retains the archived credential identity while
// rebinding its authentication to the credential used to provision the target.
// The import has exclusive ownership; other credentials are left archived.
func PatchControllerCredential(ctx context.Context, db *sql.DB, modelUUID, authType string, attributes map[string]string) error {
	type input struct {
		ModelUUID  string `db:"model_uuid"`
		UUID       string `db:"uuid"`
		AuthType   string `db:"auth_type"`
		AuthTypeID int    `db:"auth_type_id"`
		Key        string `db:"key"`
		Value      string `db:"value"`
	}
	in := input{ModelUUID: modelUUID, AuthType: authType}
	tx, err := sqlair.NewDB(db).Begin(ctx, nil)
	if err != nil {
		return errors.Capture(err)
	}
	defer tx.Rollback()
	query, err := sqlair.Prepare(`
SELECT m.cloud_credential_uuid AS &input.uuid, at.id AS &input.auth_type_id
FROM model AS m JOIN auth_type AS at ON at.type = $input.auth_type
WHERE m.uuid = $input.model_uuid`, in)
	if err != nil {
		return errors.Capture(err)
	}
	if err := tx.Query(ctx, query, in).Get(&in); err != nil {
		return errors.Errorf("finding controller credential: %w", err)
	}
	for _, statement := range []string{
		`UPDATE cloud_credential AS cc SET auth_type_id = $input.auth_type_id,
revoked = false, invalid = false, invalid_reason = NULL WHERE cc.uuid = $input.uuid`,
		`DELETE FROM cloud_credential_attribute AS cca WHERE cca.cloud_credential_uuid = $input.uuid`,
	} {
		query, err := sqlair.Prepare(statement, in)
		if err != nil {
			return errors.Capture(err)
		}
		if err := tx.Query(ctx, query, in).Run(); err != nil {
			return errors.Capture(err)
		}
	}
	query, err = sqlair.Prepare(`
INSERT INTO cloud_credential_attribute (cloud_credential_uuid, key, value)
VALUES ($input.uuid, $input.key, $input.value)`, in)
	if err != nil {
		return errors.Capture(err)
	}
	for key, value := range attributes {
		in.Key, in.Value = key, value
		if err := tx.Query(ctx, query, in).Run(); err != nil {
			return errors.Capture(err)
		}
	}
	return errors.Capture(tx.Commit())
}
