// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package restore

import (
	"time"

	"github.com/juju/juju/core/semversion"
)

// ModelInfo describes a single model recorded in a backup archive.
type ModelInfo struct {
	// UUID is the model's logical identity, preserved by restore.
	UUID string

	// Name is the model's name.
	Name string

	// ModelType is "iaas" or "caas".
	ModelType string

	// CloudName is the name of the model's cloud.
	CloudName string

	// CloudType is the provider family of the model's cloud, for example
	// "lxd", "ec2" or "kubernetes".
	CloudType string
}

// ArchiveInfo is the validated summary of a controller backup archive.
// It is produced before anything is provisioned and drives the restore
// preflight checks.
type ArchiveInfo struct {
	// AgentVersion is the exact agent version of the source controller,
	// read from the archive metadata (the manifest).
	AgentVersion semversion.Number

	// ControllerUUID is the source controller's logical identity.
	ControllerUUID string

	// ControllerName is the source controller's name as recorded in the
	// controller configuration. Kubernetes restores must bootstrap with
	// this name so the controller namespace keeps its source name. It is
	// empty when the archive does not record it.
	ControllerName string

	// ControllerModelUUID is the source controller model's identity.
	ControllerModelUUID string

	// HANodes is the number of controller nodes the source had. Restore
	// always produces a single-node replacement; values above one mean
	// the remaining controller machines load as dead rows.
	HANodes int64

	// BackupFinished is when the source backup completed, telling the
	// operator how much state drift to expect.
	BackupFinished time.Time

	// CloudName is the name of the controller model's cloud.
	CloudName string

	// CloudType is the provider family of the controller model's cloud.
	// Restore requires the replacement to use the same provider family.
	CloudType string

	// Models is the full model inventory of the source controller,
	// including the controller model.
	Models []ModelInfo

	// Checksum is the archive's SHA-256 checksum, hex encoded, computed
	// while streaming the archive.
	Checksum string

	// Size is the archive file's size in bytes.
	Size int64
}
