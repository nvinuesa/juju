// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package restore

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"io"
	"os"
	"path"
	"strings"

	"github.com/juju/juju/internal/errors"
)

// Stage copies every archive file-object blob referenced by the restored
// object-store metadata into a private staging tree rooted at stageDir, laid
// out as objectstore/<namespace>/<sha384>. Blobs are verified against their
// SHA-384 path key, expected size, and SHA-256 checksum before they are
// written. Missing or corrupt referenced blobs are fatal; valid unreferenced
// blobs are ignored. The same content hash may appear in several namespaces.
//
// root.tar is never unpacked wholesale: only the objectstore subtree is
// consumed, and nothing else in the archive is extracted.
func Stage(ctx context.Context, opener archiveOpener, archive *Archive, stageDir string) error {
	r, err := opener()
	if err != nil {
		return errors.Errorf("opening backup archive for staging: %w", err)
	}
	defer r.Close()

	gr, err := gzip.NewReader(r)
	if err != nil {
		return errors.Errorf("backup archive is not gzip: %w: %v", ErrArchiveInvalid, err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)

	// Find root.tar in the outer archive.
	var found bool
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return errors.Errorf("reading backup archive: %w: %v", ErrArchiveInvalid, err)
		}
		if hdr.Name == path.Join(contentDir, filesBundle) {
			found = true
			break
		}
		if _, err := io.Copy(io.Discard, tr); err != nil {
			return errors.Errorf("reading backup archive: %w: %v", ErrArchiveInvalid, err)
		}
	}
	if !found {
		return errors.Errorf("archive missing %s: %w", filesBundle, ErrArchiveInvalid)
	}

	// Stream the nested root.tar: the outer reader is positioned at the
	// start of the root.tar entry, whose content is itself a tar stream.
	ntr := tar.NewReader(tr)
	found384 := map[string]bool{}
	seen := map[string]bool{}
	for {
		hdr, err := ntr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return errors.Errorf("reading root.tar: %w: %v", ErrArchiveInvalid, err)
		}
		ns, sha384, ok := objectStorePath(hdr.Name)
		if !ok {
			// Non-objectstore root member: never extracted.
			if _, err := io.Copy(io.Discard, ntr); err != nil {
				return errors.Errorf("reading root.tar: %w: %v", ErrArchiveInvalid, err)
			}
			continue
		}
		if err := validateEntry(hdr); err != nil {
			return err
		}
		if seen[hdr.Name] {
			return errors.Errorf("duplicate root.tar entry %q: %w", hdr.Name, ErrArchiveInvalid)
		}
		seen[hdr.Name] = true

		meta, required := archive.ObjectStoreMetadata[sha384]
		if !required {
			// Valid unreferenced blob: warn and ignore.
			if _, err := io.Copy(io.Discard, ntr); err != nil {
				return errors.Errorf("reading root.tar: %w: %v", ErrArchiveInvalid, err)
			}
			continue
		}
		dest := path.Join(stageDir, objectStoreDir, ns, sha384)
		if err := os.MkdirAll(path.Dir(dest), 0700); err != nil {
			return errors.Errorf("creating staging directory: %w", err)
		}
		if err := stageBlob(ntr, dest, meta); err != nil {
			return errors.Errorf("staging blob %q: %w", sha384, err)
		}
		found384[sha384] = true
	}

	// Every referenced object must have been staged.
	var missing []string
	for sha384 := range archive.ObjectStoreMetadata {
		if !found384[sha384] {
			missing = append(missing, sha384)
		}
	}
	if len(missing) > 0 {
		return errors.Errorf("archive is missing %d referenced object blob(s) %v: %w",
			len(missing), missing, ErrArchiveInvalid)
	}
	return nil
}

// objectStorePath splits an archive root.tar path into (namespace, sha384)
// when it names a file object-store blob, i.e. ends with
// objectstore/<namespace>/<sha384>. The data-dir prefix is ignored so the
// check is robust to source data-dir differences.
func objectStorePath(name string) (namespace, sha384 string, ok bool) {
	idx := strings.LastIndex(name, objectStoreDir+"/")
	if idx < 0 {
		return "", "", false
	}
	rel := name[idx+len(objectStoreDir)+1:]
	parts := strings.Split(rel, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// stageBlob verifies a blob's size, SHA-384 key, and SHA-256 checksum while
// copying it to dest with restrictive permissions. The stream is bounded at
// meta.Size+1 before it ever touches the disk, so a malformed archive cannot
// grow the staging tree past the expected size.
func stageBlob(r io.Reader, dest string, meta ObjectStoreMetadata) error {
	sha384h := sha512.New384()
	sha256h := sha256.New()
	multi := io.MultiWriter(sha384h, sha256h)

	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return errors.Errorf("creating staged file: %w", err)
	}
	// A mismatch (wrong size/hash) drops the partial file; only the verified
	// blob survives staging.
	remove := true
	defer func() {
		if remove {
			_ = os.Remove(dest)
		}
	}()

	written, err := io.Copy(multi, io.TeeReader(io.LimitReader(r, meta.Size+1), f))
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return errors.Errorf("writing staged file: %w", err)
	}
	if written != meta.Size {
		return errors.Errorf("blob size %d does not match metadata %d", written, meta.Size)
	}
	got384 := hex.EncodeToString(sha384h.Sum(nil))
	if got384 != meta.SHA384 {
		return errors.Errorf("blob SHA-384 %q does not match metadata %q", got384, meta.SHA384)
	}
	got256 := hex.EncodeToString(sha256h.Sum(nil))
	if got256 != meta.SHA256 {
		return errors.Errorf("blob SHA-256 %q does not match metadata %q", got256, meta.SHA256)
	}
	remove = false
	return nil
}
