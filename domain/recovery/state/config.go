// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package state

import (
	"context"

	"github.com/canonical/sqlair"

	coreobjectstore "github.com/juju/juju/core/objectstore"
	"github.com/juju/juju/internal/errors"
)

// Controller config keys describing the object store backend. The
// archived S3 settings belong to the lost source; left in place they
// would outlive the backend row, which recovery never imports.
const (
	objectStoreTypeKey         = "object-store-type"
	objectStoreS3EndpointKey   = "object-store-s3-endpoint"
	objectStoreS3StaticKey     = "object-store-s3-static-key"
	objectStoreS3StaticSecret  = "object-store-s3-static-secret"
	objectStoreS3StaticSession = "object-store-s3-static-session"
)

// PatchControllerObjectStoreConfig forces the recovered controller's
// object store configuration to its file backend and removes every
// archived S3 setting. Recovery is always file-backed: the replacement
// keeps its own backend row, and this patch keeps the controller config
// consistent with it, so nothing reconciles the store back to the
// source's S3. Returning to S3 is the operator's explicit transition
// afterwards, with fresh credentials.
func (st *State) PatchControllerObjectStoreConfig(ctx context.Context) error {
	type configValue struct {
		Key   string `db:"key"`
		Value string `db:"value"`
	}
	type configKeys []string

	upsert, err := st.Prepare(`
INSERT INTO controller_config ("key", value)
VALUES ($configValue.key, $configValue.value)
ON CONFLICT("key") DO UPDATE SET value = excluded.value`, configValue{})
	if err != nil {
		return errors.Capture(err)
	}
	remove, err := st.Prepare(`
DELETE FROM controller_config WHERE "key" IN ($configKeys[:])`, configKeys{})
	if err != nil {
		return errors.Capture(err)
	}

	db, err := st.DB(ctx)
	if err != nil {
		return errors.Capture(err)
	}
	return errors.Capture(db.Txn(ctx, func(ctx context.Context, tx *sqlair.TX) error {
		if err := tx.Query(ctx, upsert, configValue{
			Key:   objectStoreTypeKey,
			Value: string(coreobjectstore.FileBackend),
		}).Run(); err != nil {
			return errors.Errorf("forcing file object store config: %w", err)
		}
		if err := tx.Query(ctx, remove, configKeys{
			objectStoreS3EndpointKey,
			objectStoreS3StaticKey,
			objectStoreS3StaticSecret,
			objectStoreS3StaticSession,
		}).Run(); err != nil {
			return errors.Errorf("removing archived S3 object store config: %w", err)
		}
		return nil
	}))
}
