// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package recovery

import (
	"context"
	"os"
	"path/filepath"

	"gopkg.in/tomb.v2"

	"github.com/juju/juju/core/flags"
	"github.com/juju/juju/internal/errors"
	internalrecovery "github.com/juju/juju/internal/recovery"
	"github.com/juju/juju/internal/worker/gate"
)

type FlagService interface {
	SetFlag(context.Context, string, bool, string) error
}

// WorkerConfig supplies the retryable finalisation operation. Completion is
// target-local: archived bootstrapped flags never skip this operation.
type WorkerConfig struct {
	DataDir     string
	ArchivePath string
	Checksum    string
	Operation   func(context.Context) error
	FlagService FlagService
	Unlocker    gate.Unlocker
}

type recoveryWorker struct {
	cfg  WorkerConfig
	tomb tomb.Tomb
}

func NewWorker(cfg WorkerConfig) (*recoveryWorker, error) {
	if cfg.DataDir == "" || cfg.Checksum == "" || cfg.Operation == nil || cfg.FlagService == nil || cfg.Unlocker == nil {
		return nil, errors.New("incomplete recovery worker configuration")
	}
	w := &recoveryWorker{cfg: cfg}
	w.tomb.Go(w.loop)
	return w, nil
}

func (w *recoveryWorker) Kill()       { w.tomb.Kill(nil) }
func (w *recoveryWorker) Wait() error { return w.tomb.Wait() }

func (w *recoveryWorker) loop() error {
	ctx := w.tomb.Context(context.Background())
	initialised, err := internalrecovery.HasMarker(w.cfg.DataDir, internalrecovery.InitialisedFile, w.cfg.Checksum)
	if err != nil {
		return errors.Capture(err)
	}
	if !initialised {
		return errors.New("recovery database initialisation has not completed")
	}
	complete, err := internalrecovery.HasMarker(w.cfg.DataDir, internalrecovery.CompletedFile, w.cfg.Checksum)
	if err != nil {
		return errors.Capture(err)
	}
	if !complete {
		if err := w.cfg.Operation(ctx); err != nil {
			return errors.Capture(err)
		}
		if err := w.cfg.FlagService.SetFlag(ctx, flags.BootstrapFlag, true, flags.BootstrapFlagDescription); err != nil {
			return errors.Capture(err)
		}
		if err := internalrecovery.WriteMarker(w.cfg.DataDir, internalrecovery.CompletedFile, w.cfg.Checksum); err != nil {
			return errors.Capture(err)
		}
	}
	// Parameters and the provisioned mode survive restarts. Only the sensitive
	// staging data is removed after durable completion.
	if err := os.RemoveAll(filepath.Join(w.cfg.DataDir, "recovery", "bundle")); err != nil {
		return errors.Capture(err)
	}
	if w.cfg.ArchivePath != "" {
		if err := os.Remove(w.cfg.ArchivePath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return errors.Capture(err)
		}
	}
	w.cfg.Unlocker.Unlock()
	return nil
}
