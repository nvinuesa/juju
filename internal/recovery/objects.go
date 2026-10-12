// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package recovery

import (
	"context"
	"crypto/sha512"
	"database/sql"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"

	recoverystate "github.com/juju/juju/domain/recovery/state"
	"github.com/juju/juju/internal/errors"
)

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
// recovery. After the install, every object_store_metadata_path row of every
// database is checked to exist in its own namespace directory: a merged
// metadata row without its blob would only fail later, on first Get.
//
// bundleRoot is the directory the archive's root.tar was unpacked into
// (mirroring the source data directory); dataDir is the replacement's data
// directory. Blobs already present with matching hash — the replacement's
// own bootstrap blobs — are skipped.
func LoadObjects(ctx context.Context, dbs []ObjectDB, bundleRoot, dataDir string) (int, error) {
	blobSet := make(map[string]recoverystate.ObjectBlob)
	for _, odb := range dbs {
		blobs, err := recoverystate.QueryObjects(ctx, odb.DB)
		if err != nil {
			return 0, errors.Capture(err)
		}
		for _, blob := range blobs {
			if existing, ok := blobSet[blob.SHA384]; ok && existing.Size != blob.Size {
				return 0, errors.Errorf(
					"object metadata conflict: sha384 %q is recorded with size %d and %d",
					blob.SHA384, existing.Size, blob.Size)
			}
			blobSet[blob.SHA384] = blob
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
		paths, err := recoverystate.ObjectMetadataPaths(ctx, odb.DB)
		if err != nil {
			return 0, errors.Capture(err)
		}
		for _, p := range paths {
			if _, err := os.Stat(filepath.Join(targetStore, odb.Namespace, p.SHA384)); errors.Is(err, os.ErrNotExist) {
				missing = append(missing, odb.Namespace+"/"+p.Path)
			}
		}
	}
	if len(missing) > 0 {
		return 0, errors.Errorf(
			"recovered object metadata references blobs that are not in the archive: %s",
			strings.Join(missing, "; "))
	}
	return copied, nil
}

// installObject verifies and installs one blob under every namespace
// directory that holds it in the bundle, returning the number of copies
// installed.
func installObject(blob recoverystate.ObjectBlob, bundleStore, targetStore string) (int, error) {
	namespaces, err := findBlobNamespaces(bundleStore, blob.SHA384)
	if err != nil {
		return 0, errors.Capture(err)
	}
	if len(namespaces) == 0 {
		return 0, errors.Errorf("object blob %q referenced by metadata is not in the archive", blob.SHA384)
	}

	installed := 0
	for _, namespace := range namespaces {
		src := filepath.Join(bundleStore, namespace, blob.SHA384)
		dest := filepath.Join(targetStore, namespace, blob.SHA384)
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
func installObjectFile(blob recoverystate.ObjectBlob, src, dest string) (bool, error) {
	if same, err := fileMatches(dest, blob); err != nil {
		return false, errors.Capture(err)
	} else if same {
		return false, nil
	}

	if same, err := fileMatches(src, blob); err != nil {
		return false, errors.Errorf("verifying archived object %q: %w", blob.SHA384, err)
	} else if !same {
		return false, errors.Errorf("archived object %q fails size or hash verification", blob.SHA384)
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return false, errors.Errorf("creating object store namespace: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".recovery-*")
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
func fileMatches(path string, blob recoverystate.ObjectBlob) (bool, error) {
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
	if stat.Size() != blob.Size {
		return false, nil
	}
	hasher := sha512.New384()
	if _, err := io.Copy(hasher, f); err != nil {
		return false, errors.Capture(err)
	}
	return hex.EncodeToString(hasher.Sum(nil)) == blob.SHA384, nil
}
