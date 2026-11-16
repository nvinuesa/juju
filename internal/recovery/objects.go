// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package recovery

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha512"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	domainrecovery "github.com/juju/juju/domain/recovery"
	"github.com/juju/juju/internal/errors"
)

// objectStoreDirName is the directory every object store root has below the
// data directory.
const objectStoreDirName = "objectstore"

// UnpackObjectStore streams the archive's data-directory files archive and
// installs its object store subtree under dataDir. It returns the number of
// regular files installed.
func UnpackObjectStore(ctx context.Context, archivePath, dataDir string) (int, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return 0, errors.Errorf("opening archive: %w", err)
	}
	defer func() { _ = f.Close() }()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return 0, errors.Errorf("reading archive: %w", err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return 0, errors.Errorf("reading archive: %w", err)
		}
		if (hdr.Typeflag == tar.TypeReg || hdr.Typeflag == tar.TypeRegA) &&
			path.Clean(hdr.Name) == canonicalPaths.FilesArchive {
			n, err := unpackObjectStoreTar(ctx, tr,
				filepath.Join(dataDir, objectStoreDirName))
			return n, errors.Capture(err)
		}
	}
	return 0, errors.Errorf("archive is missing %s", canonicalPaths.FilesArchive)
}

// unpackObjectStoreTar writes the object-store entries of an uncompressed
// tar stream into destDir. Entry names are validated before any path is
// constructed; entries outside the object store root are skipped, as are
// special entries within it: a skipped blob the metadata references fails
// the completeness check after unpacking.
func unpackObjectStoreTar(ctx context.Context, r io.Reader, destDir string) (int, error) {
	tr := tar.NewReader(r)
	seen := make(map[string]struct{})
	installed := 0
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return installed, nil
		}
		if err != nil {
			return installed, errors.Errorf("reading object archive: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return installed, errors.Capture(err)
		}
		if strings.HasPrefix(hdr.Name, "/") || slices.Contains(strings.Split(hdr.Name, "/"), "..") {
			return installed, errors.Errorf("object archive contains unsafe path %q", hdr.Name)
		}
		name := path.Clean(hdr.Name)

		// The files archive mirrors the source data directory, whose own
		// path may contain an "objectstore" component; the store root is
		// the final one. Entries outside it are not unpacked.
		segments := strings.Split(name, "/")
		root := -1
		for i := len(segments) - 1; i >= 0; i-- {
			if segments[i] == objectStoreDirName {
				root = i
				break
			}
		}
		if root < 0 || root == len(segments)-1 {
			continue
		}
		rel := path.Join(segments[root+1:]...)
		if _, dup := seen[rel]; dup {
			return installed, errors.Errorf("object archive contains duplicate path %q", name)
		}
		seen[rel] = struct{}{}

		target := filepath.Join(destDir, filepath.FromSlash(rel))
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o700); err != nil {
				return installed, errors.Errorf("creating directory %q: %w", name, err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return installed, errors.Errorf("creating directory for %q: %w", name, err)
			}
			out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
			if err != nil {
				return installed, errors.Errorf("creating file %q: %w", name, err)
			}
			if _, err := io.Copy(out, tr); err != nil {
				_ = out.Close()
				return installed, errors.Errorf("writing file %q: %w", name, err)
			}
			if err := out.Close(); err != nil {
				return installed, errors.Errorf("writing file %q: %w", name, err)
			}
			installed++
		default:
			continue
		}
	}
}

// CheckObjectStore verifies that every object metadata row recorded in the
// recovered databases resolves to a blob in the replacement's object store.
// A complete local object set is a hard requirement for recovery: a metadata
// row without its blob would only fail later, on first Get.
func CheckObjectStore(ctx context.Context, inventories []domainrecovery.ObjectInventory, dataDir string) error {
	var missing []string
	for _, inventory := range inventories {
		if err := ctx.Err(); err != nil {
			return errors.Capture(err)
		}
		for _, p := range inventory.Paths {
			if !isHexSHA384(p.SHA384) {
				return errors.Errorf(
					"recovered object metadata references an invalid blob hash %q (namespace %q, path %q)",
					p.SHA384, inventory.Namespace, p.Path)
			}
			blob := filepath.Join(dataDir, objectStoreDirName, inventory.Namespace, p.SHA384)
			info, err := os.Stat(blob)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return errors.Capture(err)
			}
			if err != nil || !info.Mode().IsRegular() {
				// A metadata row without a path is still reachable by
				// hash; report the hash.
				name := p.Path
				if name == "" {
					name = p.SHA384
				}
				missing = append(missing, inventory.Namespace+"/"+name)
			}
		}
	}
	if len(missing) > 0 {
		return errors.Errorf(
			"recovered object metadata references blobs that are not in the archive: %s",
			strings.Join(missing, "; "))
	}
	return nil
}

// isHexSHA384 reports whether s is a hex-encoded SHA-384 hash.
func isHexSHA384(s string) bool {
	if len(s) != 2*sha512.Size384 {
		return false
	}
	for i := range len(s) {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}
