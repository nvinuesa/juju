// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package restore

import (
	"bytes"
	"context"

	"gopkg.in/yaml.v3"

	"github.com/juju/juju/controller"
	corebackups "github.com/juju/juju/core/backups"
	"github.com/juju/juju/core/semversion"
	"github.com/juju/juju/internal/errors"
)

// controllerDumpPayload mirrors the fields of the controller database
// dump that preflight validation needs. Unknown fields are ignored; the
// full decode happens agent-side during the load stage.
type controllerDumpPayload struct {
	Cloud []struct {
		UUID        string `yaml:"uuid"`
		Name        string `yaml:"name"`
		CloudTypeID int64  `yaml:"cloud_type_id"`
	} `yaml:"cloud"`
	CloudType []struct {
		ID   *int64 `yaml:"id"`
		Type string `yaml:"type"`
	} `yaml:"cloud_type"`
	Model []struct {
		UUID        string `yaml:"uuid"`
		Name        string `yaml:"name"`
		CloudUUID   string `yaml:"cloud_uuid"`
		ModelTypeID int64  `yaml:"model_type_id"`
	} `yaml:"model"`
	ModelType []struct {
		ID   *int64 `yaml:"id"`
		Type string `yaml:"type"`
	} `yaml:"model_type"`
	ControllerConfig []struct {
		Key   string `yaml:"key"`
		Value string `yaml:"value"`
	} `yaml:"controller_config"`
}

// controllerDumpEnvelope is the top-level shape of controller.yaml. The
// envelope version is intentionally not parsed here: the agent-version
// gate implies matching dump formats, and the loader validates them.
type controllerDumpEnvelope struct {
	Payload controllerDumpPayload `yaml:"payload"`
}

// ValidateArchive reads the backup archive at archivePath and returns
// its validated summary. When expectedSHA256 is not empty it must match
// the archive's SHA-256 checksum (hex): the expected value always comes
// from the operator, never from inside the archive.
//
// Validation is offline and read-only: it runs on the bootstrap client
// before anything is provisioned.
func ValidateArchive(ctx context.Context, archivePath, expectedSHA256 string) (*ArchiveInfo, error) {
	contents, checksum, size, err := readArchive(ctx, archivePath, expectedSHA256)
	if err != nil {
		return nil, errors.Capture(err)
	}

	meta, err := corebackups.NewMetadataJSONReader(bytes.NewReader(contents.metadata))
	if err != nil {
		return nil, errors.Errorf("parsing %s: %w", metadataPath, err)
	}
	if meta.Origin.Version == corebackups.UnknownVersion ||
		meta.Origin.Version == (semversion.Number{}) {
		return nil, errors.Errorf("%s does not record the source agent version", metadataPath)
	}

	var dump controllerDumpEnvelope
	if err := yaml.Unmarshal(contents.controllerDump, &dump); err != nil {
		return nil, errors.Errorf("parsing %s: %w", controllerDumpPath, err)
	}

	info, err := buildArchiveInfo(meta, &dump.Payload, contents.modelDumps)
	if err != nil {
		return nil, errors.Capture(err)
	}
	info.Checksum = checksum
	info.Size = size
	return info, nil
}

// buildArchiveInfo resolves clouds and model types from the controller
// dump and cross-checks the model dump inventory against the model table.
func buildArchiveInfo(meta *corebackups.Metadata, payload *controllerDumpPayload, modelDumps map[string][]byte) (*ArchiveInfo, error) {
	cloudTypes := make(map[int64]string)
	for _, ct := range payload.CloudType {
		if ct.ID != nil {
			cloudTypes[*ct.ID] = ct.Type
		}
	}
	modelTypes := make(map[int64]string)
	for _, mt := range payload.ModelType {
		if mt.ID != nil {
			modelTypes[*mt.ID] = mt.Type
		}
	}
	type cloudRef struct {
		name     string
		provider string
	}
	clouds := make(map[string]cloudRef)
	for _, cl := range payload.Cloud {
		clouds[cl.UUID] = cloudRef{name: cl.Name, provider: cloudTypes[cl.CloudTypeID]}
	}

	info := &ArchiveInfo{
		AgentVersion:        meta.Origin.Version,
		ControllerUUID:      meta.Controller.UUID,
		ControllerModelUUID: meta.Origin.Model,
		HANodes:             meta.Controller.HANodes,
	}
	for _, cc := range payload.ControllerConfig {
		if cc.Key == controller.ControllerName {
			info.ControllerName = cc.Value
		}
	}

	if len(payload.Model) == 0 {
		return nil, errors.Errorf("%s records no models", controllerDumpPath)
	}
	for _, m := range payload.Model {
		cloud, ok := clouds[m.CloudUUID]
		if !ok {
			return nil, errors.Errorf("model %q references unknown cloud %q", m.Name, m.CloudUUID)
		}
		mi := ModelInfo{
			UUID:      m.UUID,
			Name:      m.Name,
			ModelType: modelTypes[m.ModelTypeID],
			CloudName: cloud.name,
			CloudType: cloud.provider,
		}
		if mi.ModelType == "" {
			return nil, errors.Errorf("model %q has unknown model type", m.Name)
		}
		if mi.CloudType == "" {
			return nil, errors.Errorf("model %q has unknown cloud type", m.Name)
		}
		info.Models = append(info.Models, mi)
		if m.UUID == info.ControllerModelUUID {
			info.CloudName = mi.CloudName
			info.CloudType = mi.CloudType
		}
	}
	if info.CloudType == "" {
		return nil, errors.Errorf("controller model %q not found in %s", info.ControllerModelUUID, controllerDumpPath)
	}

	// The dump inventory must match the model table exactly: a dump
	// without a model row, or a model without its dump, means the
	// archive is incomplete.
	for _, mi := range info.Models {
		if _, ok := modelDumps[mi.UUID]; !ok {
			return nil, errors.Errorf("archive is missing the database dump for model %q (%s)", mi.Name, mi.UUID)
		}
	}
	for uuid := range modelDumps {
		found := false
		for _, mi := range info.Models {
			if mi.UUID == uuid {
				found = true
				break
			}
		}
		if !found {
			return nil, errors.Errorf("archive contains a database dump for unknown model %q", uuid)
		}
	}
	return info, nil
}

// CheckAgentVersion enforces the exact-version restore gate: an archive
// is only ever restored onto the same agent version it was taken from.
// The official build number (the ".1" in "4.1-beta3.1") distinguishes
// released packaging of the same version, not the dump format or the
// schema; the gate ignores it, mirroring the CAAS agent-version
// comparison.
func (i *ArchiveInfo) CheckAgentVersion(current semversion.Number) error {
	archived := i.AgentVersion
	archived.Build = 0
	current.Build = 0
	if archived.Compare(current) != 0 {
		return errors.Errorf(
			"archive was created by agent version %s but this binary is %s; "+
				"restore requires the exact same version", i.AgentVersion, current)
	}
	return nil
}

// ModelFamily returns the single model type ("iaas" or "caas") shared by
// every model in the archive. A source controller with mixed model types
// is unsupported and rejected.
func (i *ArchiveInfo) ModelFamily() (string, error) {
	family := i.Models[0].ModelType
	for _, m := range i.Models[1:] {
		if m.ModelType != family {
			return "", errors.Errorf(
				"source controller has mixed model types: model %q is %s, expected %s",
				m.Name, m.ModelType, family)
		}
	}
	return family, nil
}

// CheckProviderFamily enforces the same-provider-family rule: the
// replacement controller must be bootstrapped on a cloud of the same
// provider type as the source controller model's cloud.
func (i *ArchiveInfo) CheckProviderFamily(targetCloudType string) error {
	if i.CloudType != targetCloudType {
		return errors.Errorf(
			"archive comes from a %q controller but the bootstrap cloud is %q; "+
				"restore requires the same provider family", i.CloudType, targetCloudType)
	}
	return nil
}
