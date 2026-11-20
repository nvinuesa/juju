// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package recovery

import (
	"os"
	"path/filepath"

	"github.com/juju/juju/internal/errors"
)

// ParamsFile is the data-directory-relative file marking a recovery target.
// It is empty in the agent-startup stage and carries the recovery parameters
// once the archive is uploaded. The generated setup creates it only when
// absent, so a restart never truncates parameters populated by a later
// recovery stage.
const ParamsFile = "recovery/params"

// IsRecovery reports whether dataDir contains recovery parameters. A missing
// file means ordinary startup; any regular file, empty or populated, marks a
// recovery target.
func IsRecovery(dataDir string) (bool, error) {
	paramsPath := filepath.Join(dataDir, ParamsFile)
	info, err := os.Stat(paramsPath)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, errors.Capture(err)
	}
	if !info.Mode().IsRegular() {
		return false, errors.Errorf(
			"invalid recovery params path %q: expected regular file", paramsPath,
		)
	}
	return true, nil
}
