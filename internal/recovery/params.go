// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package recovery

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/juju/juju/core/machine"
	"github.com/juju/juju/core/semversion"
	"github.com/juju/juju/internal/cloudconfig/instancecfg"
	"github.com/juju/juju/internal/errors"
)

const (
	// ParamsFile is the data-directory-relative file marking a recovery
	// target. It is empty in the agent-startup stage and carries the
	// recovery parameters once the archive is uploaded.
	ParamsFile = "recovery/params"

	// ArchiveFile is the data-directory-relative path of the uploaded
	// recovery archive.
	ArchiveFile = "recovery/archive.tar.gz"

	// StartedFile records that database initialisation was claimed. It is
	// retained on failure so an interrupted import is never replayed.
	StartedFile = "recovery/initialisation-started"
)

// Params holds the recovery inputs delivered with the replacement instance.
// Node carries the shared node configuration; it does not select or invoke
// the fresh bootstrap workflow.
type Params struct {
	ArchivePath  string                                `yaml:"archive-path"`
	SHA256       string                                `yaml:"sha256"`
	AgentVersion semversion.Number                     `yaml:"agent-version"`
	MachineName  string                                `yaml:"machine-name"`
	Node         instancecfg.StateInitializationParams `yaml:"-"`
	NodeConfig   string                                `yaml:"node-config"`
}

// Marshal returns the recovery parameters with the node configuration
// embedded.
func (p Params) Marshal() ([]byte, error) {
	data, err := p.Node.Marshal()
	if err != nil {
		return nil, errors.Capture(err)
	}
	p.NodeConfig = string(data)
	return yaml.Marshal(p)
}

// ReadParams reads and validates the recovery parameters from dataDir.
func ReadParams(dataDir string) (Params, error) {
	var p Params
	data, err := os.ReadFile(filepath.Join(dataDir, ParamsFile))
	if err != nil {
		return p, errors.Errorf("reading recovery parameters: %w", err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return p, errors.New("incomplete recovery parameters")
	}
	if err := yaml.Unmarshal(data, &p); err != nil {
		return p, errors.Capture(err)
	}
	if err := p.Node.Unmarshal([]byte(p.NodeConfig)); err != nil {
		return p, errors.Capture(err)
	}
	sum := strings.TrimPrefix(p.SHA256, "sha256-")
	if p.ArchivePath == "" || len(sum) != 64 || p.AgentVersion == semversion.Zero ||
		p.MachineName == "" || p.Node.AgentVersion != p.AgentVersion {
		return p, errors.New("incomplete recovery parameters")
	}
	if _, err := hex.DecodeString(sum); err != nil {
		return p, errors.New("invalid recovery checksum")
	}
	if err := machine.Name(p.MachineName).Validate(); err != nil {
		return p, errors.Capture(err)
	}
	return p, nil
}

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

// HasMarker reports whether the named marker exists and belongs to the
// archive with the given checksum. A marker for another archive is an error:
// recovery must never adopt another attempt's partial state.
func HasMarker(dataDir, name, checksum string) (bool, error) {
	data, err := os.ReadFile(filepath.Join(dataDir, name))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, errors.Capture(err)
	}
	if strings.TrimSpace(string(data)) != checksum {
		return false, errors.New("recovery marker belongs to another archive")
	}
	return true, nil
}

// BeginInitialisation records exclusive ownership before database import.
// The marker is retained on failure: partially imported databases must never
// be replayed or opened by another initialiser.
func BeginInitialisation(dataDir, checksum string) error {
	filename := filepath.Join(dataDir, StartedFile)
	if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
		return errors.Capture(err)
	}
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return errors.Errorf("claiming recovery initialisation: %w", err)
	}
	defer f.Close()
	if _, err := f.WriteString(checksum + "\n"); err != nil {
		return errors.Capture(err)
	}
	if err := f.Sync(); err != nil {
		return errors.Capture(err)
	}
	dir, err := os.Open(filepath.Dir(filename))
	if err != nil {
		return errors.Capture(err)
	}
	defer dir.Close()
	return errors.Capture(dir.Sync())
}
