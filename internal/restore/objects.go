// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package restore

import (
	"context"
	"crypto/sha512"
	"database/sql"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/juju/juju/internal/errors"
)

// objectBlob describes one object-store blob recorded in the loaded
// controller database.
type objectBlob struct {
	sha384 string
	size   int64
}

// ObjectDB associates a loaded database with its object-store namespace:
// the directory under <dataDir>/objectstore that holds the database's
// blobs ("controller" for the controller database, the model UUID for a
// model database).
type ObjectDB struct {
	Namespace string
	DB        *sql.DB
}

// LoadObjects copies the archived object-store blobs into the replacement's
// file-backed object store. Object metadata lives in the controller and model
// databases; every recorded blob is verified against its declared SHA-384
// and size, because a complete local object set is a hard requirement for
// restore. After the install, every object_store_metadata_path row of every
// database is checked to exist in its own namespace directory: a merged
// metadata row without its blob would only fail later, on first Get.
//
// bundleRoot is the directory the archive's root.tar was unpacked into
// (mirroring the source data directory); dataDir is the replacement's data
// directory. Blobs already present with matching hash — the replacement's
// own bootstrap blobs — are skipped.
func LoadObjects(ctx context.Context, dbs []ObjectDB, bundleRoot, dataDir string) (int, error) {
	blobSet := make(map[string]objectBlob)
	for _, odb := range dbs {
		blobs, err := queryObjects(ctx, odb.DB)
		if err != nil {
			return 0, errors.Capture(err)
		}
		for _, blob := range blobs {
			if existing, ok := blobSet[blob.sha384]; ok && existing.size != blob.size {
				return 0, errors.Errorf(
					"object metadata conflict: sha384 %q is recorded with size %d and %d",
					blob.sha384, existing.size, blob.size)
			}
			blobSet[blob.sha384] = blob
		}
	}

	// The bundle mirrors the source filesystem root: the backup walks
	// the source data directory (/var/lib/juju) under its absolute path,
	// so the object store lands at var/lib/juju/objectstore below the
	// unpack root.
	bundleStore := filepath.Join(bundleRoot, "var", "lib", "juju", "objectstore")
	targetStore := filepath.Join(dataDir, "objectstore")

	copied := 0
	for _, blob := range blobSet {
		if err := ctx.Err(); err != nil {
			return 0, errors.Capture(err)
		}
		n, err := installObject(blob, bundleStore, targetStore)
		if err != nil {
			return 0, errors.Capture(err)
		}
		copied += n
	}

	// A metadata path row merged from the archive promises a Get at that
	// path succeeds after the load. The physical file is the metadata's
	// sha384 inside the database's own namespace directory; anything
	// missing here is a deferred failure, so fail the load now.
	var missing []string
	for _, odb := range dbs {
		paths, err := objectMetadataPaths(ctx, odb.DB)
		if err != nil {
			return 0, errors.Capture(err)
		}
		for _, p := range paths {
			if _, err := os.Stat(filepath.Join(targetStore, odb.Namespace, p.sha384)); errors.Is(err, os.ErrNotExist) {
				missing = append(missing, odb.Namespace+"/"+p.path)
			}
		}
	}
	if len(missing) > 0 {
		return 0, errors.Errorf(
			"restored object metadata references blobs that are not in the archive: %s",
			strings.Join(missing, "; "))
	}
	return copied, nil
}

// objectMetadataPath pairs a merged metadata path with its blob hash.
type objectMetadataPath struct {
	path   string
	sha384 string
}

// objectMetadataPaths reads every object_store_metadata_path row of one
// loaded database, joined with its metadata hash.
func objectMetadataPaths(ctx context.Context, db *sql.DB) ([]objectMetadataPath, error) {
	rows, err := db.QueryContext(ctx, `
SELECT osmp.path, osm.sha_384
FROM object_store_metadata_path osmp
JOIN object_store_metadata osm ON osmp.metadata_uuid = osm.uuid`)
	if err != nil {
		return nil, errors.Errorf("querying object store metadata paths: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var paths []objectMetadataPath
	for rows.Next() {
		var p objectMetadataPath
		if err := rows.Scan(&p.path, &p.sha384); err != nil {
			return nil, errors.Errorf("reading object store metadata paths: %w", err)
		}
		paths = append(paths, p)
	}
	return paths, errors.Capture(rows.Err())
}

// queryObjects reads every object-store metadata row from one loaded
// database.
func queryObjects(ctx context.Context, db *sql.DB) ([]objectBlob, error) {
	rows, err := db.QueryContext(ctx,
		"SELECT sha_384, size FROM object_store_metadata")
	if err != nil {
		return nil, errors.Errorf("querying object store metadata: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var blobs []objectBlob
	for rows.Next() {
		var blob objectBlob
		if err := rows.Scan(&blob.sha384, &blob.size); err != nil {
			return nil, errors.Errorf("reading object store metadata: %w", err)
		}
		blobs = append(blobs, blob)
	}
	return blobs, errors.Capture(rows.Err())
}

// installObject verifies and installs one blob under every namespace
// directory that holds it in the bundle, returning the number of copies
// installed.
func installObject(blob objectBlob, bundleStore, targetStore string) (int, error) {
	namespaces, err := findBlobNamespaces(bundleStore, blob.sha384)
	if err != nil {
		return 0, errors.Capture(err)
	}
	if len(namespaces) == 0 {
		return 0, errors.Errorf("object blob %q referenced by metadata is not in the archive", blob.sha384)
	}

	installed := 0
	for _, namespace := range namespaces {
		src := filepath.Join(bundleStore, namespace, blob.sha384)
		dest := filepath.Join(targetStore, namespace, blob.sha384)
		ok, err := installObjectFile(blob, src, dest)
		if err != nil {
			return 0, errors.Capture(err)
		}
		if ok {
			installed++
		}
	}
	return installed, nil
}

// findBlobNamespaces returns the bundle objectstore namespace directories
// containing a file named hash.
func findBlobNamespaces(bundleStore, hash string) ([]string, error) {
	entries, err := os.ReadDir(bundleStore)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Errorf("reading object store bundle: %w", err)
	}
	var namespaces []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(bundleStore, entry.Name(), hash)); err == nil {
			namespaces = append(namespaces, entry.Name())
		}
	}
	return namespaces, nil
}

// installObjectFile verifies src against the declared size and SHA-384
// and installs it at dest with restricted permissions, atomically. A dest
// already holding the same content — the replacement's bootstrap blobs —
// is left untouched.
func installObjectFile(blob objectBlob, src, dest string) (bool, error) {
	if same, err := fileMatches(dest, blob); err != nil {
		return false, errors.Capture(err)
	} else if same {
		return false, nil
	}

	if same, err := fileMatches(src, blob); err != nil {
		return false, errors.Errorf("verifying archived object %q: %w", blob.sha384, err)
	} else if !same {
		return false, errors.Errorf("archived object %q fails size or hash verification", blob.sha384)
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return false, errors.Errorf("creating object store namespace: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".restore-*")
	if err != nil {
		return false, errors.Errorf("staging object: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	in, err := os.Open(src)
	if err != nil {
		_ = tmp.Close()
		return false, errors.Capture(err)
	}
	if _, err := io.Copy(tmp, in); err != nil {
		_ = in.Close()
		_ = tmp.Close()
		return false, errors.Errorf("copying object: %w", err)
	}
	if err := in.Close(); err != nil {
		_ = tmp.Close()
		return false, errors.Capture(err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return false, errors.Capture(err)
	}
	if err := tmp.Close(); err != nil {
		return false, errors.Capture(err)
	}
	if err := os.Rename(tmp.Name(), dest); err != nil {
		return false, errors.Errorf("installing object: %w", err)
	}
	return true, nil
}

// fileMatches reports whether path exists, has the declared size and
// hashes to the declared SHA-384.
func fileMatches(path string, blob objectBlob) (bool, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, errors.Capture(err)
	}
	defer func() { _ = f.Close() }()

	stat, err := f.Stat()
	if err != nil {
		return false, errors.Capture(err)
	}
	if stat.Size() != blob.size {
		return false, nil
	}
	hasher := sha512.New384()
	if _, err := io.Copy(hasher, f); err != nil {
		return false, errors.Capture(err)
	}
	return hex.EncodeToString(hasher.Sum(nil)) == blob.sha384, nil
}
