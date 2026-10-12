// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package state

import (
	"context"

	"github.com/canonical/sqlair"

	"github.com/juju/juju/internal/errors"
)

// PatchControllerCredential retains the archived credential identity while
// rebinding its authentication to the credential used to provision the target.
// The import has exclusive ownership; other credentials are left archived.
func (st *State) PatchControllerCredential(ctx context.Context, modelUUID, authType string, attributes map[string]string) error {
	type model struct {
		UUID           string `db:"uuid"`
		CredentialUUID string `db:"cloud_credential_uuid"`
	}
	input := model{UUID: modelUUID}
	query, err := st.Prepare(`SELECT m.cloud_credential_uuid AS &model.cloud_credential_uuid
FROM model AS m WHERE m.uuid = $model.uuid`, input)
	if err != nil {
		return errors.Capture(err)
	}
	db, err := st.DB(ctx)
	if err != nil {
		return errors.Capture(err)
	}
	var found model
	err = db.Txn(ctx, func(ctx context.Context, tx *sqlair.TX) error {
		return tx.Query(ctx, query, input).Get(&found)
	})
	if err != nil {
		return errors.Capture(err)
	}
	return errors.Capture(st.PatchCloudCredential(ctx, found.CredentialUUID, authType, attributes))
}

// PatchCloudCredential preserves a credential's UUID while rebinding the
// authentication selected for its model cloud during recovery preflight.
func (st *State) PatchCloudCredential(ctx context.Context, credentialUUID, authType string, attributes map[string]string) error {
	type credential struct {
		UUID       string `db:"uuid"`
		AuthType   string `db:"auth_type"`
		AuthTypeID int    `db:"auth_type_id"`
		Key        string `db:"key"`
		Value      string `db:"value"`
	}
	in := credential{UUID: credentialUUID, AuthType: authType}
	lookup, err := st.Prepare(`
SELECT cc.uuid AS &credential.uuid, at.id AS &credential.auth_type_id
FROM cloud_credential AS cc JOIN auth_type AS at ON at.type = $credential.auth_type
WHERE cc.uuid = $credential.uuid`, in)
	if err != nil {
		return errors.Capture(err)
	}
	update, err := st.Prepare(`
UPDATE cloud_credential AS cc SET auth_type_id = $credential.auth_type_id,
revoked = false, invalid = false, invalid_reason = NULL WHERE cc.uuid = $credential.uuid`, in)
	if err != nil {
		return errors.Capture(err)
	}
	deleteAttrs, err := st.Prepare(`
DELETE FROM cloud_credential_attribute AS cca WHERE cca.cloud_credential_uuid = $credential.uuid`, in)
	if err != nil {
		return errors.Capture(err)
	}
	insertAttr, err := st.Prepare(`
INSERT INTO cloud_credential_attribute (cloud_credential_uuid, key, value)
VALUES ($credential.uuid, $credential.key, $credential.value)`, in)
	if err != nil {
		return errors.Capture(err)
	}
	db, err := st.DB(ctx)
	if err != nil {
		return errors.Capture(err)
	}
	return errors.Capture(db.Txn(ctx, func(ctx context.Context, tx *sqlair.TX) error {
		in := in
		if err := tx.Query(ctx, lookup, in).Get(&in); err != nil {
			return errors.Errorf("finding controller credential: %w", err)
		}
		if err := tx.Query(ctx, update, in).Run(); err != nil {
			return errors.Capture(err)
		}
		if err := tx.Query(ctx, deleteAttrs, in).Run(); err != nil {
			return errors.Capture(err)
		}
		for key, value := range attributes {
			in.Key, in.Value = key, value
			if err := tx.Query(ctx, insertAttr, in).Run(); err != nil {
				return errors.Capture(err)
			}
		}
		return nil
	}))
}
