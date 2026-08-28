// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package v4_1_0

import (
	"database/sql/driver"
	"encoding/base64"
	"fmt"

	"gopkg.in/yaml.v3"
)

// Binary preserves SQLite BLOB values through YAML export and import.
type Binary string

// Scan implements [sql.Scanner].
func (b *Binary) Scan(value any) error {
	switch value := value.(type) {
	case []byte:
		*b = Binary(value)
	case string:
		*b = Binary(value)
	default:
		return fmt.Errorf("cannot scan %T into Binary", value)
	}
	return nil
}

// Value implements [driver.Valuer].
func (b Binary) Value() (driver.Value, error) {
	return []byte(b), nil
}

// MarshalYAML preserves the binary YAML representation used by backups.
func (b Binary) MarshalYAML() (any, error) {
	return &yaml.Node{
		Kind:  yaml.ScalarNode,
		Tag:   "!!binary",
		Value: base64.StdEncoding.EncodeToString([]byte(b)),
	}, nil
}

// UnmarshalYAML decodes the binary YAML representation used by backups.
func (b *Binary) UnmarshalYAML(node *yaml.Node) error {
	if node.Tag != "!!binary" {
		return fmt.Errorf("cannot decode YAML tag %q into Binary", node.Tag)
	}
	value, err := base64.StdEncoding.DecodeString(node.Value)
	if err != nil {
		return fmt.Errorf("decoding binary YAML: %w", err)
	}
	*b = Binary(value)
	return nil
}
