// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package restore

import (
	"github.com/juju/juju/internal/errors"
)

// Error classes for the restore pipeline. Each phase maps onto a distinct
// class so the command can report *what* went wrong and *when* before any
// database mutation. Any error raised after the first mutation must be
// reported with the "keep the controller stopped and rebootstrap/retry"
// guidance, because v1 has no rollback/resume.
const (
	// ErrUsage reports a malformed command line.
	ErrUsage = errors.ConstError("invalid restore-backup usage")

	// ErrArchiveInvalid reports a malformed, unsafe, or structurally invalid
	// archive. It is raised during preflight, before any mutation.
	ErrArchiveInvalid = errors.ConstError("invalid backup archive")

	// ErrSourceUnsupported reports source state restore cannot handle
	// (HA topology, incompatible version, active S3/drain, ...).
	ErrSourceUnsupported = errors.ConstError("source backup not supported by restore")

	// ErrTargetIncompatible reports a target that restore cannot safely
	// replace (live controller, nonfresh model state, cloud/credential
	// mismatch, missing local mapping, ...).
	ErrTargetIncompatible = errors.ConstError("target controller incompatible with restore")

	// ErrStaging reports failure to stage archive blobs before mutation.
	ErrStaging = errors.ConstError("staging backup object store")

	// ErrMutation reports a failure during database import/overlay after
	// mutation has begun. The controller must stay stopped.
	ErrMutation = errors.ConstError("restore database mutation failed")

	// ErrFinalization reports a failure during object-store swap or
	// configuration regeneration, after imports completed.
	ErrFinalization = errors.ConstError("restore finalization failed")
)
