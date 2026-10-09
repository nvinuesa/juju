// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package recovery

import (
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
	ModeFile        = "recovery/is-recovery"
	ParamsFile      = "recovery/params"
	StartedFile     = "recovery/initialisation-started"
	InitialisedFile = "recovery/initialisation-complete"
	CompletedFile   = "recovery/completed"
)

// Params belongs to recovery. Node contains the shared node configuration;
// it does not select or invoke the fresh bootstrap workflow.
type Params struct {
	ArchivePath  string
	SHA256       string
	AgentVersion semversion.Number
	MachineName  string
	MachineNonce string
	Node         instancecfg.StateInitializationParams `yaml:"-"`
	NodeConfig   string
}

func (p Params) Marshal() ([]byte, error) {
	data, err := p.Node.Marshal()
	if err != nil {
		return nil, errors.Capture(err)
	}
	p.NodeConfig = string(data)
	return yaml.Marshal(p)
}

func ReadParams(dataDir string) (Params, error) {
	var p Params
	data, err := os.ReadFile(filepath.Join(dataDir, ParamsFile))
	if err != nil {
		return p, errors.Errorf("reading recovery parameters: %w", err)
	}
	if err := yaml.Unmarshal(data, &p); err != nil {
		return p, errors.Capture(err)
	}
	if err := p.Node.Unmarshal([]byte(p.NodeConfig)); err != nil {
		return p, errors.Capture(err)
	}
	if p.ArchivePath == "" || len(p.SHA256) != 64 || p.AgentVersion == semversion.Zero || p.MachineName == "" || p.Node.AgentVersion != p.AgentVersion {
		return p, errors.New("incomplete recovery parameters")
	}
	if _, err := hex.DecodeString(p.SHA256); err != nil {
		return p, errors.New("invalid recovery checksum")
	}
	if err := machine.Name(p.MachineName).Validate(); err != nil {
		return p, errors.Capture(err)
	}
	return p, nil
}

// IsRecovery reads provisioned provenance. A missing file denotes ordinary
// startup; other read failures and malformed values must not select bootstrap.
func IsRecovery(dataDir string) (bool, error) {
	data, err := os.ReadFile(filepath.Join(dataDir, ModeFile))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, errors.Errorf("reading recovery mode: %w", err)
	}
	switch strings.TrimSpace(string(data)) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, errors.New("invalid recovery mode")
	}
}

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

// WriteMarker atomically persists completion before consumers can proceed.
func WriteMarker(dataDir, name, checksum string) error {
	filename := filepath.Join(dataDir, name)
	if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
		return errors.Capture(err)
	}
	f, err := os.CreateTemp(filepath.Dir(filename), ".marker-")
	if err != nil {
		return errors.Capture(err)
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(checksum + "\n"); err != nil {
		f.Close()
		return errors.Capture(err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return errors.Capture(err)
	}
	if err := f.Close(); err != nil {
		return errors.Capture(err)
	}
	if err := os.Rename(f.Name(), filename); err != nil {
		return errors.Capture(err)
	}
	dir, err := os.Open(filepath.Dir(filename))
	if err != nil {
		return errors.Capture(err)
	}
	defer dir.Close()
	return errors.Capture(dir.Sync())
}
