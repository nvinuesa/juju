// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package recovery

import (
	"slices"

	"github.com/juju/juju/domain/application"
)

// DiscardS3Authorisation prevents an archived approval from authorising a new
// transition after recovery. Only the controller charm's option is reset. The
// archived input remains reusable if import fails and a new target is created.
func DiscardS3Authorisation(dump *ModelDump) *ModelDump {
	controllers := make(map[string]bool)
	for _, row := range dump.ApplicationController {
		controllers[row.ApplicationUUID] = true
	}
	filtered := *dump
	configs := slices.Clone(dump.ApplicationConfig)
	changed := make(map[string]bool)
	for i := range configs {
		row := &configs[i]
		if controllers[row.ApplicationUUID] && row.Key == "authorize-s3-transition" {
			row.Value = new("false")
			changed[row.ApplicationUUID] = true
		}
	}
	filtered.ApplicationConfig = configs
	hashes := slices.Clone(dump.ApplicationConfigHash)
	for i := range hashes {
		row := &hashes[i]
		app := row.ApplicationUUID
		if !changed[app] {
			continue
		}
		values := make(map[string]string)
		for _, cfg := range configs {
			if cfg.ApplicationUUID != app {
				continue
			}
			values[cfg.Key] = stringOf(cfg.Value)
		}
		var trust bool
		for _, settings := range dump.ApplicationSetting {
			if settings.ApplicationUUID == app {
				trust = boolOf(settings.Trust)
			}
		}
		row.Sha256 = application.ConfigHash(values, trust)
	}
	filtered.ApplicationConfigHash = hashes
	return &filtered
}

func boolOf(b *bool) bool {
	if b == nil {
		return false
	}
	return *b
}
