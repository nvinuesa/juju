// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package restore

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/juju/juju/internal/errors"
)

const (
	// contentDir is the single top-level directory inside a backup
	// archive. It mirrors the layout constant in core/backups; the
	// restore reader never consumes entries outside it.
	contentDir = "juju-backup"

	// metadataPath is the archive's metadata file: the manifest that
	// records the source agent version.
	metadataPath = contentDir + "/metadata.json"

	// controllerDumpPath is the controller database dump.
	controllerDumpPath = contentDir + "/dump/controller.yaml"

	// modelDumpDir holds one dump per model, named by model UUID.
	modelDumpDir = contentDir + "/dump/models/"

	// maxMetadataSize bounds the metadata read from an archive.
	maxMetadataSize = 4 << 20

	// maxDumpSize bounds a single database dump held in memory while
	// validating an archive.
	maxDumpSize = 1 << 30
)

// MaxDumpSize is the largest single database dump the restore loader
// accepts: the loader rejects any dump exceeding it, so backup stages its
// dumps against the same bound and fails fast instead of producing an
// archive that no restore can load.
const MaxDumpSize = maxDumpSize

// archiveContents holds the files extracted while streaming an archive.
type archiveContents struct {
	metadata       []byte
	controllerDump []byte
	modelDumps     map[string][]byte
}

// readArchive streams archivePath once, computing the archive's SHA-256
// checksum and extracting only the metadata and database dumps. The
// object blob bundle is streamed through the hash but never held in
// memory.
//
// The reader rejects absolute paths, parent traversal, duplicate entries
// and non-regular files: an archive is operator-supplied but never
// trusted beyond its checksum.
func readArchive(ctx context.Context, archivePath, expectedSHA256 string) (*archiveContents, string, int64, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return nil, "", 0, errors.Errorf("opening archive: %w", err)
	}
	defer func() { _ = f.Close() }()

	hasher := sha256.New()
	gz, err := gzip.NewReader(io.TeeReader(f, hasher))
	if err != nil {
		return nil, "", 0, errors.Errorf("reading archive: %w", err)
	}

	contents := &archiveContents{modelDumps: make(map[string][]byte)}
	seen := make(map[string]struct{})
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, "", 0, errors.Errorf("reading archive: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return nil, "", 0, errors.Capture(err)
		}

		// Reject traversal on the raw entry name: cleaning alone would
		// silently neutralize it instead of failing the archive.
		if strings.HasPrefix(hdr.Name, "/") || slices.Contains(strings.Split(hdr.Name, "/"), "..") {
			return nil, "", 0, errors.Errorf("archive contains unsafe path %q", hdr.Name)
		}
		name := path.Clean(hdr.Name)
		if _, dup := seen[name]; dup {
			return nil, "", 0, errors.Errorf("archive contains duplicate path %q", name)
		}
		seen[name] = struct{}{}

		switch hdr.Typeflag {
		case tar.TypeDir:
			continue
		case tar.TypeReg, tar.TypeRegA:
		default:
			return nil, "", 0, errors.Errorf("archive entry %q is not a regular file", name)
		}

		var limit int64
		var store func([]byte)
		switch {
		case name == metadataPath:
			limit = maxMetadataSize
			store = func(b []byte) { contents.metadata = b }
		case name == controllerDumpPath:
			limit = maxDumpSize
			store = func(b []byte) { contents.controllerDump = b }
		case strings.HasPrefix(name, modelDumpDir) && strings.HasSuffix(name, ".yaml"):
			model := strings.TrimSuffix(strings.TrimPrefix(name, modelDumpDir), ".yaml")
			if model == "" || strings.Contains(model, "/") {
				return nil, "", 0, errors.Errorf("unexpected model dump path %q", name)
			}
			limit = maxDumpSize
			store = func(b []byte) { contents.modelDumps[model] = b }
		default:
			// root.tar and anything else is hashed but not held.
			if _, err := io.Copy(io.Discard, tr); err != nil {
				return nil, "", 0, errors.Errorf("reading %q: %w", name, err)
			}
			continue
		}

		data, err := readLimited(tr, limit)
		if err != nil {
			return nil, "", 0, errors.Errorf("reading %q: %w", name, err)
		}
		store(data)
	}

	checksum := hex.EncodeToString(hasher.Sum(nil))
	if expectedSHA256 != "" && !strings.EqualFold(checksum, expectedSHA256) {
		return nil, "", 0, errors.Errorf(
			"archive checksum mismatch: expected sha256 %q, archive is %q", expectedSHA256, checksum)
	}
	if len(contents.metadata) == 0 {
		return nil, "", 0, errors.Errorf("archive is missing %s", metadataPath)
	}
	if len(contents.controllerDump) == 0 {
		return nil, "", 0, errors.Errorf("archive is missing %s", controllerDumpPath)
	}

	stat, err := os.Stat(archivePath)
	if err != nil {
		return nil, "", 0, errors.Capture(err)
	}
	return contents, checksum, stat.Size(), nil
}

// readLimited reads r fully, failing when it holds more than limit bytes.
func readLimited(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, errors.Capture(err)
	}
	if int64(len(data)) > limit {
		return nil, errors.Errorf("file exceeds %d bytes", limit)
	}
	return data, nil
}

// UnpackObjectsBundle streams the archive once and unpacks only the
// object blob bundle (root.tar) into destDir, applying the same entry
// hardening as the metadata read: directories and regular files only, no
// absolute paths, parent traversal, duplicates or links. Files land with
// owner-only permissions; archived executables, agent configs and raw
// database files are never executed or trusted.
func UnpackObjectsBundle(ctx context.Context, archivePath, destDir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return errors.Errorf("opening archive: %w", err)
	}
	defer func() { _ = f.Close() }()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return errors.Errorf("reading archive: %w", err)
	}

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return errors.Errorf("reading archive: %w", err)
		}
		if (hdr.Typeflag == tar.TypeReg || hdr.Typeflag == tar.TypeRegA) &&
			path.Clean(hdr.Name) == contentDir+"/root.tar" {
			return errors.Capture(unpackTar(ctx, tr, destDir))
		}
	}
	return errors.Errorf("archive is missing %s/root.tar", contentDir)
}

// unpackTar writes the entries of an uncompressed tar stream into
// destDir. Entry names are validated before any path is constructed.
func unpackTar(ctx context.Context, r io.Reader, destDir string) error {
	tr := tar.NewReader(r)
	seen := make(map[string]struct{})
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return errors.Errorf("reading object bundle: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return errors.Capture(err)
		}
		if strings.HasPrefix(hdr.Name, "/") || slices.Contains(strings.Split(hdr.Name, "/"), "..") {
			return errors.Errorf("object bundle contains unsafe path %q", hdr.Name)
		}
		name := path.Clean(hdr.Name)
		if _, dup := seen[name]; dup {
			return errors.Errorf("object bundle contains duplicate path %q", name)
		}
		seen[name] = struct{}{}

		target := filepath.Join(destDir, filepath.FromSlash(name))
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o700); err != nil {
				return errors.Errorf("creating directory %q: %w", name, err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return errors.Errorf("creating directory for %q: %w", name, err)
			}
			out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if err != nil {
				return errors.Errorf("creating file %q: %w", name, err)
			}
			if _, err := io.Copy(out, tr); err != nil {
				_ = out.Close()
				return errors.Errorf("writing file %q: %w", name, err)
			}
			if err := out.Close(); err != nil {
				return errors.Errorf("writing file %q: %w", name, err)
			}
		default:
			// Symlinks (k8s controllers run their tools binaries from
			// charm-bin symlinks) and other special entries are not
			// consumed: restore installs only referenced object-store
			// blobs, which are regular files under the objectstore
			// namespace directories. Skip them and keep unpacking the
			// rest of the bundle: returning here would silently
			// truncate every entry after the first special file.
			continue
		}
	}
}
