// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package recovery

import (
	"bytes"
	"context"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"

	"github.com/juju/juju/cloud"
	"github.com/juju/juju/controller"
	"github.com/juju/juju/core/arch"
	corebackups "github.com/juju/juju/core/backups"
	"github.com/juju/juju/core/base"
	"github.com/juju/juju/core/semversion"
	exportcontroller "github.com/juju/juju/domain/export/types/controller/v4_1_0"
	exportmodel "github.com/juju/juju/domain/export/types/v4_1_0"
	domainlife "github.com/juju/juju/domain/life"
	domainrecovery "github.com/juju/juju/domain/recovery"
	"github.com/juju/juju/internal/errors"
)

// controllerDumpPayload mirrors the fields of the controller database
// dump that preflight validation needs. Unknown fields are ignored; the
// full decode happens agent-side during the load stage.
type controllerDumpPayload struct {
	Controller []struct {
		UUID           string  `yaml:"uuid"`
		ModelUUID      string  `yaml:"model_uuid"`
		CACert         *string `yaml:"ca_cert"`
		CAPrivateKey   *string `yaml:"ca_private_key"`
		SystemIdentity *string `yaml:"system_identity"`
	} `yaml:"controller"`
	Cloud                    []exportcontroller.Cloud                    `yaml:"cloud"`
	CloudRegion              []exportcontroller.CloudRegion              `yaml:"cloud_region"`
	CloudAuthType            []exportcontroller.CloudAuthType            `yaml:"cloud_auth_type"`
	CloudCaCert              []exportcontroller.CloudCaCert              `yaml:"cloud_ca_cert"`
	CloudCredential          []exportcontroller.CloudCredential          `yaml:"cloud_credential"`
	CloudCredentialAttribute []exportcontroller.CloudCredentialAttribute `yaml:"cloud_credential_attribute"`
	AuthType                 []exportcontroller.AuthType                 `yaml:"auth_type"`
	CloudType                []struct {
		ID   *int64 `yaml:"id"`
		Type string `yaml:"type"`
	} `yaml:"cloud_type"`
	Model     []exportcontroller.Model `yaml:"model"`
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
// loader validates the envelope format independently of agent version.
type controllerDumpEnvelope struct {
	Payload controllerDumpPayload `yaml:"payload"`
}

// ValidateArchive reads the backup archive at archivePath and returns
// its validated summary. When expectedSHA256 is not empty it must match
// the archive's SHA-256 checksum (hex): the expected value always comes
// from the operator, never from inside the archive.
//
// Validation is offline and runs before anything is provisioned. Model
// dumps are staged in temporary files that are removed before returning.
func ValidateArchive(ctx context.Context, archivePath, expectedSHA256 string) (*domainrecovery.ArchiveInfo, error) {
	contents, info, err := ReadArchive(ctx, archivePath, expectedSHA256)
	if err != nil {
		return nil, err
	}
	if err := contents.Close(); err != nil {
		return nil, errors.Errorf("cleaning up staged recovery dumps: %w", err)
	}
	return info, nil
}

// ReadArchive reads and validates the backup archive at archivePath and
// returns both the extracted contents — metadata and database dumps —
// and the validated summary. The agent-side recovery stage consumes the
// contents; the offline preflight consumes the summary alone via
// [ValidateArchive]. The caller must Close the contents once consumed.
func ReadArchive(ctx context.Context, archivePath, expectedSHA256 string) (*ArchiveContents, *domainrecovery.ArchiveInfo, error) {
	contents, checksum, size, err := readArchive(ctx, archivePath, expectedSHA256)
	if err != nil {
		return nil, nil, errors.Capture(err)
	}
	info, err := parseArchiveInfo(ctx, contents)
	if err != nil {
		_ = contents.Close()
		return nil, nil, errors.Capture(err)
	}
	info.Checksum = checksum
	info.Size = size
	return contents, info, nil
}

// parseArchiveInfo builds the archive summary from extracted contents:
// metadata, controller dump and the model dump inventory.
func parseArchiveInfo(ctx context.Context, contents *ArchiveContents) (*domainrecovery.ArchiveInfo, error) {
	meta, err := corebackups.NewMetadataJSONReader(bytes.NewReader(contents.Metadata))
	if err != nil {
		return nil, errors.Errorf("parsing %s: %w", metadataPath, err)
	}
	if meta.Origin.Version == corebackups.UnknownVersion ||
		meta.Origin.Version == (semversion.Number{}) {
		return nil, errors.Errorf("%s does not record the source agent version", metadataPath)
	}

	var dump controllerDumpEnvelope
	if err := yaml.Unmarshal(contents.ControllerDump, &dump); err != nil {
		return nil, errors.Errorf("parsing %s: %w", controllerDumpPath, err)
	}

	info, err := buildArchiveInfo(ctx, meta, &dump.Payload, contents.ModelDumps)
	if err != nil {
		return nil, errors.Capture(err)
	}
	if meta.Finished != nil {
		info.BackupFinished = *meta.Finished
	}
	return info, nil
}

// buildArchiveInfo resolves clouds and model types from the controller
// dump and cross-checks the model dump inventory against the model table.
func buildArchiveInfo(ctx context.Context, meta *corebackups.Metadata, payload *controllerDumpPayload, modelDumps map[string]string) (*domainrecovery.ArchiveInfo, error) {
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

	info := &domainrecovery.ArchiveInfo{
		AgentVersion:        meta.Origin.Version,
		SourceBase:          meta.Origin.Base,
		MachineName:         meta.Origin.Machine,
		ControllerConfig:    make(map[string]string),
		ControllerUUID:      meta.Controller.UUID,
		ControllerModelUUID: meta.Origin.Model,
		HANodes:             meta.Controller.HANodes,
	}
	for _, cc := range payload.ControllerConfig {
		info.ControllerConfig[cc.Key] = cc.Value
		if cc.Key == controller.ControllerName {
			info.ControllerName = cc.Value
		}
	}

	if len(payload.Controller) == 0 {
		return nil, errors.Errorf("%s records no controller row", controllerDumpPath)
	}
	if payload.Controller[0].CACert == nil || payload.Controller[0].CAPrivateKey == nil {
		return nil, errors.Errorf("%s records no controller CA material", controllerDumpPath)
	}
	if payload.Controller[0].SystemIdentity != nil {
		info.SystemIdentity = *payload.Controller[0].SystemIdentity
	}
	info.CACert = *payload.Controller[0].CACert
	info.CAPrivateKey = *payload.Controller[0].CAPrivateKey
	if payload.Controller[0].ModelUUID != "" &&
		info.ControllerModelUUID != payload.Controller[0].ModelUUID {
		return nil, errors.Errorf(
			"controller model mismatch: metadata records %q, controller row records %q",
			info.ControllerModelUUID, payload.Controller[0].ModelUUID)
	}
	// The manifest and the dump must agree on the controller's identity:
	// the recovered controller adopts the manifest's uuid, so a divergent
	// dump row means the archive is internally inconsistent.
	if payload.Controller[0].UUID != "" && payload.Controller[0].UUID != info.ControllerUUID {
		return nil, errors.Errorf(
			"controller uuid mismatch: metadata records %q, controller row records %q",
			info.ControllerUUID, payload.Controller[0].UUID)
	}

	if len(payload.Model) == 0 {
		return nil, errors.Errorf("%s records no models", controllerDumpPath)
	}
	seenModels := make(map[string]struct{}, len(payload.Model))
	for _, m := range payload.Model {
		if _, dup := seenModels[m.UUID]; dup {
			return nil, errors.Errorf("dump records duplicate model uuid %q", m.UUID)
		}
		seenModels[m.UUID] = struct{}{}
		cloud, ok := clouds[m.CloudUUID]
		if !ok {
			return nil, errors.Errorf("model %q references unknown cloud %q", m.Name, m.CloudUUID)
		}
		mi := domainrecovery.ModelInfo{
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
			if err := resolveRecoveryTarget(info, payload, m); err != nil {
				return nil, err
			}
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

	var modelDump struct {
		Payload struct {
			ModelConfig           []struct{ Key, Value string }       `yaml:"model_config"`
			ApplicationController []exportmodel.ApplicationController `yaml:"application_controller"`
			ApplicationPlatform   []exportmodel.ApplicationPlatform   `yaml:"application_platform"`
			Architecture          []exportmodel.Architecture          `yaml:"architecture"`
			OS                    []exportmodel.Os                    `yaml:"os"`
		} `yaml:"payload"`
	}
	file, err := os.Open(modelDumps[info.ControllerModelUUID])
	if err != nil {
		return nil, errors.Errorf("reading controller model configuration: %w", err)
	}
	defer func() { _ = file.Close() }()
	if err := yaml.NewDecoder(contextReader{ctx: ctx, reader: file}).Decode(&modelDump); err != nil {
		return nil, errors.Errorf("parsing controller model configuration: %w", err)
	}
	info.ModelConfig = make(map[string]string)
	for _, attr := range modelDump.Payload.ModelConfig {
		info.ModelConfig[attr.Key] = attr.Value
	}

	if info.CloudType == cloud.CloudTypeKubernetes {
		payload := modelDump.Payload
		if len(payload.ApplicationController) != 1 {
			return nil, errors.New("archive must identify one Kubernetes controller application")
		}
		for _, platform := range payload.ApplicationPlatform {
			if platform.ApplicationUUID != payload.ApplicationController[0].ApplicationUUID {
				continue
			}
			for _, a := range payload.Architecture {
				if a.ID != nil && *a.ID == platform.ArchitectureID {
					info.ControllerArchitecture = a.Name
				}
			}
			for _, os := range payload.OS {
				if os.ID != nil && strconv.FormatInt(*os.ID, 10) == platform.OsID && platform.Channel != nil {
					info.ControllerCharmBase = os.Name + "@" + *platform.Channel
				}
			}
		}
		if !arch.AllArches().Contains(info.ControllerArchitecture) {
			return nil, errors.New("archive does not record a supported Kubernetes controller architecture")
		}
		if _, err := base.ParseBaseFromString(info.ControllerCharmBase); err != nil {
			return nil, errors.Errorf("invalid archived controller charm base: %w", err)
		}
	}

	// Fill the CAAS workload inventory from the model dumps: it powers
	// the read-only substrate check, which reports missing workload
	// objects instead of silently recreating them at the first
	// reconcile. The controller model is skipped: its namespace is
	// disposable bootstrap output, not surviving substrate.
	for i := range info.Models {
		mi := &info.Models[i]
		if mi.ModelType != "caas" || mi.UUID == info.ControllerModelUUID {
			continue
		}
		apps, err := inventoryFromModelDump(ctx, modelDumps[mi.UUID])
		if err != nil {
			return nil, errors.Errorf("model %q: %w", mi.Name, err)
		}
		mi.Applications = apps
	}
	return info, nil
}

func resolveRecoveryTarget(info *domainrecovery.ArchiveInfo, payload *controllerDumpPayload, model exportcontroller.Model) error {
	for _, cloud := range payload.Cloud {
		if cloud.UUID != model.CloudUUID {
			continue
		}
		info.Cloud = domainrecovery.CloudInfo{
			Name: cloud.Name, Type: info.CloudType, Endpoint: cloud.Endpoint,
			IdentityEndpoint: stringValue(cloud.IdentityEndpoint), StorageEndpoint: stringValue(cloud.StorageEndpoint),
			SkipTLSVerify: cloud.SkipTlsVerify,
		}
	}
	authTypes := make(map[int64]string)
	for _, auth := range payload.AuthType {
		if auth.ID != nil && auth.Type != nil {
			authTypes[*auth.ID] = *auth.Type
		}
	}
	for _, auth := range payload.CloudAuthType {
		if auth.CloudUUID != model.CloudUUID {
			continue
		}
		name := authTypes[auth.AuthTypeID]
		if name == "" {
			return errors.New("archived cloud references an unknown authentication type")
		}
		info.Cloud.AuthTypes = append(info.Cloud.AuthTypes, name)
	}
	for _, cert := range payload.CloudCaCert {
		if cert.CloudUUID == model.CloudUUID {
			info.Cloud.CACertificates = append(info.Cloud.CACertificates, cert.CaCert)
		}
	}
	for _, region := range payload.CloudRegion {
		if region.CloudUUID != model.CloudUUID {
			continue
		}
		info.Cloud.Regions = append(info.Cloud.Regions, domainrecovery.RegionInfo{
			Name: region.Name, Endpoint: stringValue(region.Endpoint),
			IdentityEndpoint: stringValue(region.IdentityEndpoint), StorageEndpoint: stringValue(region.StorageEndpoint),
		})
		if model.CloudRegionUUID != nil && region.UUID == *model.CloudRegionUUID {
			info.Region = region.Name
		}
	}
	if model.CloudRegionUUID != nil && info.Region == "" {
		return errors.New("controller model references an unknown cloud region")
	}
	if model.CloudRegionUUID == nil && len(info.Cloud.Regions) != 0 {
		return errors.New("controller model does not identify its source cloud region")
	}
	if model.CloudCredentialUUID == nil {
		return nil
	}
	for _, credential := range payload.CloudCredential {
		if credential.UUID != *model.CloudCredentialUUID {
			continue
		}
		if credential.CloudUUID != model.CloudUUID {
			return errors.New("controller credential belongs to a different cloud")
		}
		id, err := strconv.ParseInt(credential.AuthTypeID, 10, 64)
		if err != nil || authTypes[id] == "" {
			return errors.New("controller credential references an unknown authentication type")
		}
		info.Credential = &domainrecovery.CredentialInfo{
			Name: credential.Name, AuthType: authTypes[id], Attributes: make(map[string]string),
			Revoked: credential.Revoked != nil && *credential.Revoked,
			Invalid: credential.Invalid != nil && *credential.Invalid,
		}
		for _, attr := range payload.CloudCredentialAttribute {
			if attr.CloudCredentialUUID == credential.UUID && attr.Value != nil {
				info.Credential.Attributes[attr.Key] = *attr.Value
			}
		}
		return nil
	}
	return errors.New("controller model references an unknown cloud credential")
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// modelDumpInventory mirrors the model-dump tables the substrate
// inventory reads. Unknown tables and fields are ignored.
//
// Life filtering excludes only dead rows: dying entities still exist
// in the cluster, so they count as surviving substrate the preflight
// must account for.
type modelDumpInventory struct {
	Application []struct {
		UUID   string          `yaml:"uuid"`
		Name   string          `yaml:"name"`
		LifeID domainlife.Life `yaml:"life_id"`
	} `yaml:"application"`
	Unit []struct {
		Name            string          `yaml:"name"`
		LifeID          domainlife.Life `yaml:"life_id"`
		ApplicationUUID string          `yaml:"application_uuid"`
		NetNodeUUID     string          `yaml:"net_node_uuid"`
	} `yaml:"unit"`
	StorageFilesystem []struct {
		UUID       string          `yaml:"uuid"`
		ProviderID string          `yaml:"provider_id"`
		LifeID     domainlife.Life `yaml:"life_id"`
	} `yaml:"storage_filesystem"`
	StorageFilesystemAttachment []struct {
		StorageFilesystemUUID string          `yaml:"storage_filesystem_uuid"`
		NetNodeUUID           string          `yaml:"net_node_uuid"`
		LifeID                domainlife.Life `yaml:"life_id"`
	} `yaml:"storage_filesystem_attachment"`
}

// inventoryFromModelDump extracts the surviving-substrate inventory of
// one CAAS model: its alive applications, their alive units, and the
// persistent volume claim names recorded by alive storage filesystems.
// A claim name is attributed to the application whose unit attaches it.
func inventoryFromModelDump(ctx context.Context, filename string) ([]domainrecovery.ApplicationInfo, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, errors.Errorf("opening dump: %w", err)
	}
	defer func() { _ = file.Close() }()

	var envelope struct {
		Payload modelDumpInventory `yaml:"payload"`
	}
	if err := yaml.NewDecoder(contextReader{ctx: ctx, reader: file}).Decode(&envelope); err != nil {
		return nil, errors.Errorf("decoding dump: %w", err)
	}
	inv := envelope.Payload

	// unitAppByNode maps a unit's net node to the unit's application,
	// resolving which application each attached volume belongs to.
	unitAppByNode := make(map[string]string, len(inv.Unit))
	for _, u := range inv.Unit {
		if u.LifeID == domainlife.Dead || u.NetNodeUUID == "" {
			continue
		}
		unitAppByNode[u.NetNodeUUID] = u.ApplicationUUID
	}
	// provider_id is the persistent volume claim name: its unique index
	// is how the running controller re-attaches volumes it finds in the
	// cluster, so it is the name the substrate check must verify.
	filesystemClaim := make(map[string]string, len(inv.StorageFilesystem))
	for _, fs := range inv.StorageFilesystem {
		if fs.ProviderID == "" || fs.LifeID == domainlife.Dead {
			continue
		}
		filesystemClaim[fs.UUID] = fs.ProviderID
	}
	claimsByApp := make(map[string][]string)
	for _, att := range inv.StorageFilesystemAttachment {
		if att.LifeID == domainlife.Dead {
			continue
		}
		claim, ok := filesystemClaim[att.StorageFilesystemUUID]
		if !ok {
			continue
		}
		if app, ok := unitAppByNode[att.NetNodeUUID]; ok && app != "" {
			claimsByApp[app] = append(claimsByApp[app], claim)
		}
	}

	inventory := make([]domainrecovery.ApplicationInfo, 0, len(inv.Application))
	for _, app := range inv.Application {
		// Dead applications are recorded in the recovery summary as
		// pending removals; their substrate is not checked.
		if app.UUID == "" || app.Name == "" || app.LifeID == domainlife.Dead {
			continue
		}
		info := domainrecovery.ApplicationInfo{
			UUID:                  app.UUID,
			Name:                  app.Name,
			FilesystemProviderIDs: claimsByApp[app.UUID],
		}
		for _, u := range inv.Unit {
			if u.ApplicationUUID == app.UUID && u.LifeID != domainlife.Dead {
				info.Units = append(info.Units, u.Name)
			}
		}
		inventory = append(inventory, info)
	}
	return inventory, nil
}
