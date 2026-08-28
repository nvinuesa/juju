// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package restore

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"os"
	"path"
	"strings"

	"gopkg.in/yaml.v3"

	corebackups "github.com/juju/juju/core/backups"
	domainexport "github.com/juju/juju/domain/export"
	"github.com/juju/juju/domain/export/types/controller/v4_1_0"
	"github.com/juju/juju/domain/export/types/latest"
	"github.com/juju/juju/internal/errors"
)

// Archive layout. The writer bundles everything under a single "juju-backup"
// content directory, then tars/gzips it. Paths inside the archive are
// canonical relative paths rooted at that directory.
const (
	contentDir   = "juju-backup"
	metadataFile = "metadata.json"
	filesBundle  = "root.tar"
	dbDumpDir    = "dump"
)

// objectStoreDir is the directory (relative to the data dir) holding the file
// object store. Blobs live at objectstore/<namespace>/<sha384>.
const objectStoreDir = "objectstore"

// maxDumpSize bounds the in-memory size of a single YAML dump member.
const maxDumpSize = 1 << 20 // 1 MiB

// Archive holds the decoded logical payload of a backup archive plus the
// object-store metadata collected across all exports. It is the product of
// preflight and the input to staging and import.
type Archive struct {
	// Metadata is the backup archive metadata.
	Metadata *corebackups.Metadata

	// Controller is the decoded controller-database export.
	Controller *v4_1_0.ControllerExport

	// Models maps source model UUID to its decoded model-DB export.
	Models map[string]*latest.ModelExport

	// ObjectStoreMetadata lists every object_store_metadata row seen across
	// the controller and model exports, keyed by SHA-384.
	ObjectStoreMetadata map[string]ObjectStoreMetadata

	// ControllerModelUUID is the source controller model UUID.
	ControllerModelUUID string

	// ControllerUUID is the source controller UUID.
	ControllerUUID string
}

// ObjectStoreMetadata is the restore-relevant subset of an
// object_store_metadata row: the hashes and size used to verify a staged blob.
type ObjectStoreMetadata struct {
	SHA256 string
	SHA384 string
	Size   int64
}

// archiveOpener returns a re-readable gzip+tar stream over the archive path.
// The archive is opened twice (preflight reads metadata/dumps, staging reads
// root.tar), so callers pass an opener rather than a single reader.
type archiveOpener func() (io.ReadCloser, error)

// OpenArchiveFile returns an opener for the named archive file.
func OpenArchiveFile(path string) archiveOpener {
	return func() (io.ReadCloser, error) {
		return os.Open(path)
	}
}

// Preflight streams the archive once, validating its structure and decoding
// the metadata and database dumps. No database mutation happens here; staging
// and import are separate phases.
func Preflight(ctx context.Context, opener archiveOpener) (*Archive, error) {
	r, err := opener()
	if err != nil {
		return nil, errors.Errorf("opening backup archive: %w", err)
	}
	defer r.Close()

	gr, err := gzip.NewReader(r)
	if err != nil {
		return nil, errors.Errorf("backup archive is not gzip: %w: %v", ErrArchiveInvalid, err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)

	seen := map[string]bool{}
	var metadataBytes, controllerBytes []byte
	modelBytes := map[string][]byte{}
	var foundFilesBundle, foundMetadata, foundController bool

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, errors.Errorf("reading backup archive: %w: %v", ErrArchiveInvalid, err)
		}
		if err := validateEntry(hdr); err != nil {
			return nil, err
		}
		if seen[hdr.Name] {
			return nil, errors.Errorf("duplicate archive entry %q: %w", hdr.Name, ErrArchiveInvalid)
		}
		seen[hdr.Name] = true
		if hdr.Typeflag == tar.TypeDir {
			continue
		}

		switch {
		case hdr.Name == path.Join(contentDir, metadataFile):
			if foundMetadata {
				return nil, errors.Errorf("duplicate metadata file: %w", ErrArchiveInvalid)
			}
			foundMetadata = true
			b, err := readEntry(tr, maxDumpSize)
			if err != nil {
				return nil, err
			}
			metadataBytes = b

		case hdr.Name == path.Join(contentDir, filesBundle):
			if foundFilesBundle {
				return nil, errors.Errorf("duplicate root.tar: %w", ErrArchiveInvalid)
			}
			foundFilesBundle = true
			// root.tar is a nested tar; its contents are streamed in the
			// staging phase. Here we only consume its header and skip the
			// (potentially large) payload.
			if _, err := io.Copy(io.Discard, tr); err != nil {
				return nil, errors.Errorf("reading root.tar: %w: %v", ErrArchiveInvalid, err)
			}

		case strings.HasPrefix(hdr.Name, path.Join(contentDir, dbDumpDir)+"/"):
			name := strings.TrimPrefix(hdr.Name, path.Join(contentDir, dbDumpDir)+"/")
			b, err := readEntry(tr, maxDumpSize)
			if err != nil {
				return nil, err
			}
			switch {
			case name == "controller.yaml":
				if foundController {
					return nil, errors.Errorf("duplicate controller dump: %w", ErrArchiveInvalid)
				}
				foundController = true
				controllerBytes = b
			case strings.HasPrefix(name, "models/") && strings.HasSuffix(name, ".yaml"):
				modelUUID := strings.TrimSuffix(strings.TrimPrefix(name, "models/"), ".yaml")
				if modelBytes[modelUUID] != nil {
					return nil, errors.Errorf("duplicate model dump %q: %w", modelUUID, ErrArchiveInvalid)
				}
				modelBytes[modelUUID] = b
			default:
				return nil, errors.Errorf("unexpected dump entry %q: %w", hdr.Name, ErrArchiveInvalid)
			}

		default:
			return nil, errors.Errorf("unexpected archive entry %q: %w", hdr.Name, ErrArchiveInvalid)
		}
	}

	if !foundMetadata {
		return nil, errors.Errorf("archive missing %s: %w", metadataFile, ErrArchiveInvalid)
	}
	if !foundFilesBundle {
		return nil, errors.Errorf("archive missing %s: %w", filesBundle, ErrArchiveInvalid)
	}
	if !foundController {
		return nil, errors.Errorf("archive missing controller dump: %w", ErrArchiveInvalid)
	}
	if len(modelBytes) == 0 {
		return nil, errors.Errorf("archive has no model dumps: %w", ErrArchiveInvalid)
	}

	meta, err := decodeMetadata(metadataBytes)
	if err != nil {
		return nil, err
	}
	controller, err := decodeController(controllerBytes)
	if err != nil {
		return nil, err
	}
	models := make(map[string]*latest.ModelExport, len(modelBytes))
	for uuid, b := range modelBytes {
		m, err := decodeModel(b)
		if err != nil {
			return nil, errors.Errorf("decoding model dump %q: %w", uuid, err)
		}
		models[uuid] = m
	}

	// Source topology / version gates.
	if err := validateSource(meta, controller, models); err != nil {
		return nil, err
	}

	objMeta := make(map[string]ObjectStoreMetadata, len(controller.ObjectStoreMetadata))
	for _, m := range controller.ObjectStoreMetadata {
		objMeta[m.Sha384] = ObjectStoreMetadata{SHA256: m.Sha256, SHA384: m.Sha384, Size: m.Size}
	}
	for _, m := range models {
		for _, osm := range m.ObjectStoreMetadata {
			objMeta[osm.Sha384] = ObjectStoreMetadata{SHA256: osm.Sha256, SHA384: osm.Sha384, Size: osm.Size}
		}
	}

	controllerUUID, controllerModelUUID, err := sourceIdentities(controller)
	if err != nil {
		return nil, err
	}

	return &Archive{
		Metadata:            meta,
		Controller:          controller,
		Models:              models,
		ObjectStoreMetadata: objMeta,
		ControllerUUID:      controllerUUID,
		ControllerModelUUID: controllerModelUUID,
	}, nil
}

// validateEntry rejects unsafe archive entries: non-relative/canonical paths,
// traversal, links, and devices. Only regular files and directories are
// accepted.
func validateEntry(hdr *tar.Header) error {
	if path.IsAbs(hdr.Name) {
		return errors.Errorf("archive entry %q has an absolute path: %w", hdr.Name, ErrArchiveInvalid)
	}
	cleaned := path.Clean(hdr.Name)
	if cleaned != hdr.Name {
		return errors.Errorf("archive entry %q is not a canonical path: %w", hdr.Name, ErrArchiveInvalid)
	}
	if strings.Contains(hdr.Name, "..") {
		return errors.Errorf("archive entry %q escapes the archive root: %w", hdr.Name, ErrArchiveInvalid)
	}
	switch hdr.Typeflag {
	case tar.TypeReg, tar.TypeDir:
		return nil
	case tar.TypeSymlink, tar.TypeLink, tar.TypeChar, tar.TypeBlock, tar.TypeFifo:
		return errors.Errorf("archive entry %q is a link or device: %w", hdr.Name, ErrArchiveInvalid)
	default:
		return errors.Errorf("archive entry %q has unsupported type %d: %w", hdr.Name, hdr.Typeflag, ErrArchiveInvalid)
	}
}

// readEntry reads an archive member into memory, bounded by maxSize.
func readEntry(tr *tar.Reader, maxSize int64) ([]byte, error) {
	buf := make([]byte, 0, 4096)
	if _, err := readInto(tr, &buf, maxSize); err != nil {
		return nil, err
	}
	return buf, nil
}

func readInto(r io.Reader, buf *[]byte, maxSize int64) (int64, error) {
	chunk := make([]byte, 32<<10)
	var total int64
	for {
		n, err := r.Read(chunk)
		if n > 0 {
			total += int64(n)
			if total > maxSize {
				return total, errors.Errorf("archive member exceeds %d bytes: %w", maxSize, ErrArchiveInvalid)
			}
			*buf = append(*buf, chunk[:n]...)
		}
		if err == io.EOF {
			return total, nil
		}
		if err != nil {
			return total, err
		}
	}
}

func decodeMetadata(b []byte) (*corebackups.Metadata, error) {
	meta, err := corebackups.NewMetadataJSONReader(strings.NewReader(string(b)))
	if err != nil {
		return nil, errors.Errorf("decoding archive metadata: %w: %v", ErrArchiveInvalid, err)
	}
	if meta.FormatVersion != 2 {
		return nil, errors.Errorf("archive metadata format %d, expected 2: %w",
			meta.FormatVersion, ErrSourceUnsupported)
	}
	return meta, nil
}

func decodeController(b []byte) (*v4_1_0.ControllerExport, error) {
	var envelope domainexport.ControllerExport
	if err := json.Unmarshal(b, &envelope); err != nil {
		if err2 := yaml.Unmarshal(b, &envelope); err2 != nil {
			return nil, errors.Errorf("decoding controller dump: %w: %v", ErrArchiveInvalid, err)
		}
	}
	payloadBytes, err := marshalPayload(envelope.Payload)
	if err != nil {
		return nil, errors.Errorf("re-encoding controller payload: %w: %v", ErrArchiveInvalid, err)
	}
	payload, err := domainexport.DecodeControllerPayload(envelope.Version, payloadBytes)
	if err != nil {
		return nil, errors.Errorf("decoding controller payload: %w: %v", ErrArchiveInvalid, err)
	}
	ctrl, ok := payload.(v4_1_0.ControllerExport)
	if !ok {
		return nil, errors.Errorf("controller payload has unexpected type: %w", ErrArchiveInvalid)
	}
	return &ctrl, nil
}

func decodeModel(b []byte) (*latest.ModelExport, error) {
	var envelope domainexport.ModelExport
	if err := json.Unmarshal(b, &envelope); err != nil {
		if err2 := yaml.Unmarshal(b, &envelope); err2 != nil {
			return nil, errors.Errorf("decoding model dump: %w: %v", ErrArchiveInvalid, err)
		}
	}
	payloadBytes, err := marshalPayload(envelope.Payload)
	if err != nil {
		return nil, errors.Errorf("re-encoding model payload: %w: %v", ErrArchiveInvalid, err)
	}
	payload, err := domainexport.DecodePayload(envelope.Version, payloadBytes)
	if err != nil {
		return nil, errors.Errorf("decoding model payload: %w: %v", ErrArchiveInvalid, err)
	}
	m, ok := payload.(latest.ModelExport)
	if !ok {
		return nil, errors.Errorf("model payload has unexpected type: %w", ErrArchiveInvalid)
	}
	return &m, nil
}

func marshalPayload(payload any) ([]byte, error) {
	return yaml.Marshal(payload)
}

// validateSource enforces the v1 restore support gate on the source archive.
func validateSource(
	meta *corebackups.Metadata,
	controller *v4_1_0.ControllerExport,
	models map[string]*latest.ModelExport,
) error {
	// Single-node source controller only.
	if meta.Controller.HANodes != 1 {
		return errors.Errorf("archive has %d HA nodes, restore supports only 1: %w",
			meta.Controller.HANodes, ErrSourceUnsupported)
	}

	// The controller export must carry a single controller row.
	if len(controller.Controller) != 1 {
		return errors.Errorf("controller export has %d controller rows, expected 1: %w",
			len(controller.Controller), ErrSourceUnsupported)
	}

	// Every model dump must correspond to a model row in the controller
	// export, and vice versa.
	modelUUIDs := make(map[string]bool, len(controller.Model))
	for _, m := range controller.Model {
		modelUUIDs[m.UUID] = true
	}
	for uuid := range models {
		if !modelUUIDs[uuid] {
			return errors.Errorf("model dump %q has no model row in the controller export: %w",
				uuid, ErrArchiveInvalid)
		}
	}
	if len(models) != len(controller.Model) {
		return errors.Errorf("model dump set (%d) does not match controller model rows (%d): %w",
			len(models), len(controller.Model), ErrArchiveInvalid)
	}

	// v1 supports a single-cloud source.
	if len(controller.Cloud) != 1 {
		return errors.Errorf("source archive has %d clouds, restore supports exactly 1: %w",
			len(controller.Cloud), ErrSourceUnsupported)
	}

	// File object store only: reject S3 backends, S3 config rows, and any
	// active drain (phases unknown/draining are active).
	for _, b := range controller.ObjectStoreBackend {
		if b.TypeID == 1 {
			return errors.Errorf("source archive references an S3 object-store backend %q: restore supports file only: %w",
				b.UUID, ErrSourceUnsupported)
		}
	}
	if len(controller.ObjectStoreBackendS3Config) > 0 {
		return errors.Errorf("source archive carries S3 object-store config: restore supports file only: %w",
			ErrSourceUnsupported)
	}
	for _, d := range controller.ObjectStoreDrainInfo {
		if d.PhaseTypeID < 2 {
			return errors.Errorf("source archive has an active object-store drain %q: %w",
				d.UUID, ErrSourceUnsupported)
		}
	}

	// Reject a controller upgrade in progress.
	if len(controller.UpgradeInfo) > 0 {
		return errors.Errorf("source archive has a controller upgrade in progress: %w",
			ErrSourceUnsupported)
	}

	// Reject a model mid-migration.
	for uuid, m := range models {
		if len(m.ModelMigrating) > 0 {
			return errors.Errorf("model %q is mid-migration: %w", uuid, ErrSourceUnsupported)
		}
	}

	// Fixed-external secret backend types (vault, any user-defined backend)
	// are not restorable: their provider tokens cannot be remapped onto a
	// different controller. Backend-type origin is integer: 0 is builtin
	// (controller/k8s), anything else is user-defined and is rejected.
	for _, sb := range controller.SecretBackend {
		if sb.OriginID != 0 {
			return errors.Errorf("external secret backend %q is not restorable: %w",
				sb.Name, ErrSourceUnsupported)
		}
	}
	return nil
}

// sourceIdentities extracts the source controller and controller-model UUIDs.
func sourceIdentities(controller *v4_1_0.ControllerExport) (ctrl, model string, err error) {
	if len(controller.Controller) != 1 {
		return "", "", errors.Errorf("controller export has %d controller rows, expected 1: %w",
			len(controller.Controller), ErrSourceUnsupported)
	}
	row := controller.Controller[0]
	if row.ModelUUID == "" {
		return "", "", errors.Errorf("controller export has no controller-model UUID: %w", ErrArchiveInvalid)
	}
	return row.UUID, row.ModelUUID, nil
}
