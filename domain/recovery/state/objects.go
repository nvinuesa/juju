// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package state

import (
	"context"
	"database/sql"

	"github.com/juju/juju/internal/errors"
)

// ObjectBlob describes one object-store blob recorded in a loaded
// database.
type ObjectBlob struct {
	SHA384 string
	Size   int64
}

// ObjectMetadataPath pairs a metadata path with its blob hash.
type ObjectMetadataPath struct {
	Path   string
	SHA384 string
}

// QueryObjects reads every object-store metadata row from one loaded
// database.
func QueryObjects(ctx context.Context, db *sql.DB) ([]ObjectBlob, error) {
	rows, err := db.QueryContext(ctx,
		"SELECT sha_384, size FROM object_store_metadata")
	if err != nil {
		return nil, errors.Errorf("querying object store metadata: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var blobs []ObjectBlob
	for rows.Next() {
		var blob ObjectBlob
		if err := rows.Scan(&blob.SHA384, &blob.Size); err != nil {
			return nil, errors.Errorf("reading object store metadata: %w", err)
		}
		blobs = append(blobs, blob)
	}
	return blobs, errors.Capture(rows.Err())
}

// ObjectMetadataPaths reads every object_store_metadata_path row of one
// loaded database, joined with its metadata hash.
func ObjectMetadataPaths(ctx context.Context, db *sql.DB) ([]ObjectMetadataPath, error) {
	rows, err := db.QueryContext(ctx, `
SELECT osmp.path, osm.sha_384
FROM object_store_metadata_path osmp
JOIN object_store_metadata osm ON osmp.metadata_uuid = osm.uuid`)
	if err != nil {
		return nil, errors.Errorf("querying object store metadata paths: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var paths []ObjectMetadataPath
	for rows.Next() {
		var p ObjectMetadataPath
		if err := rows.Scan(&p.Path, &p.SHA384); err != nil {
			return nil, errors.Errorf("reading object store metadata paths: %w", err)
		}
		paths = append(paths, p)
	}
	return paths, errors.Capture(rows.Err())
}
