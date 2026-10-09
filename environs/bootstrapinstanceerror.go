// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package environs

import (
	"fmt"

	"github.com/juju/juju/core/instance"
	"github.com/juju/juju/internal/errors"
)

// BootstrapInstanceError marks a bootstrap failure that happened after the
// bootstrap instance started, recording that instance's id. Cleanup after a
// failed bootstrap must not run a full environ destroy on a recovery (the
// replacement carries the source's model uuid, which the fenced source
// controller's resources match too): the instance id delimits exactly what
// this bootstrap created in the cloud.
type BootstrapInstanceError struct {
	// InstanceID is the provider instance id of the bootstrap instance
	// that started before the failure.
	InstanceID string

	// Err is the failure itself.
	Err error
}

// Error implements error.
func (e *BootstrapInstanceError) Error() string {
	return fmt.Sprintf("bootstrap failed after instance %q started: %v", e.InstanceID, e.Err)
}

// Unwrap returns the wrapped failure.
func (e *BootstrapInstanceError) Unwrap() error {
	return e.Err
}

// BootstrapInstanceID reports whether err is, or wraps, a
// BootstrapInstanceError, and extracts the started bootstrap instance's id.
func BootstrapInstanceID(err error) (instance.Id, bool) {
	var instanceErr *BootstrapInstanceError
	if errors.As(err, &instanceErr) {
		return instance.Id(instanceErr.InstanceID), true
	}
	return "", false
}
