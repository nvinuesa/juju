// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package podcfg

// ControllerInitialisation supplies a separate controller initialisation
// workflow. Files are seeded under Directory in the first pod's data volume;
// SetupCommand runs before the normal agent starts and must fail on errors.
type ControllerInitialisation struct {
	Directory    string
	Files        map[string]string
	SetupCommand string
}
