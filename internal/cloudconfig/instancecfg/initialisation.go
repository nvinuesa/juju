// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package instancecfg

import "time"

// ControllerInitialisation describes a workflow run before the controller
// agent starts. Prepare is called after the provider supplies instance facts.
// It must return the parameters to write without modifying agent state.
type ControllerInitialisation struct {
	Command    string
	ParamsPath string
	ModePath   string
	Mode       string
	Timeout    time.Duration
	Prepare    func(*InstanceConfig) ([]byte, error)
	Files      []InitialisationFile
}

// InitialisationFile is copied to the target before initialisation starts.
// The destination is published only after verifying SHA256.
type InitialisationFile struct {
	Source      string
	Destination string
	SHA256      string
}
