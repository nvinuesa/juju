// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package restore

import (
	"gopkg.in/yaml.v3"

	"github.com/juju/juju/internal/errors"
)

// Row is one exported database row: column name to value.
type Row map[string]any

// Dump is a decoded database dump: table name to rows.
type Dump struct {
	// Version is the export payload version that produced the dump.
	Version string

	// Tables maps table name to exported rows.
	Tables map[string][]Row
}

// dumpEnvelope mirrors the export wire shape: a version and a payload
// whose keys are table names.
type dumpEnvelope struct {
	Version string           `yaml:"version"`
	Payload map[string][]Row `yaml:"payload"`
}

// DecodeDump parses a YAML database dump into tables. Column names in
// rows are database column names; values are YAML scalars, with
// timestamps decoded as time.Time by the YAML library.
func DecodeDump(data []byte) (*Dump, error) {
	var env dumpEnvelope
	if err := yaml.Unmarshal(data, &env); err != nil {
		return nil, errors.Errorf("decoding dump: %w", err)
	}
	if len(env.Payload) == 0 {
		return nil, errors.Errorf("decoding dump: empty payload")
	}
	return &Dump{Version: env.Version, Tables: env.Payload}, nil
}
