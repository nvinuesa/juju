// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

// Package provisionertaskfactory provides a factory for selecting between
// different provisioner task implementations.
package provisionertaskfactory

import (
	"context"
	"time"

	"github.com/juju/errors"
	"github.com/juju/worker/v4"

	"github.com/juju/juju/core/logger"
	"github.com/juju/juju/internal/provisionertask"
	"github.com/juju/juju/internal/provisionertaskbis"
)

// Implementation constants define the allowed provisioner task implementations.
const (
	// ImplLegacy selects the original provisioner task implementation.
	// This is the default when no implementation is specified.
	ImplLegacy = "legacy"

	// ImplFSM selects the new FSM-based provisioner task implementation.
	ImplFSM = "fsm"
)

// ProvisionerTask represents a running provisioner task worker.
type ProvisionerTask interface {
	worker.Worker

	// SetNumProvisionWorkers resizes the pool of provision workers.
	SetNumProvisionWorkers(numWorkers int)
}

// Config holds configuration for the provisioner task factory.
// It embeds the legacy TaskConfig and adds fields needed for the FSM implementation.
type Config struct {
	provisionertask.TaskConfig

	// AvailabilityZones is used by the FSM implementation for zone-aware placement.
	// If empty, the FSM implementation will operate without zone awareness.
	AvailabilityZones []string
}

// NewProvisionerTask creates a ProvisionerTask using the specified implementation.
//
// The impl parameter selects which implementation to use:
//   - "" or "legacy": uses internal/provisionertask (default)
//   - "fsm": uses internal/provisionertaskbis (new FSM-based implementation)
//
// Returns an error if an unknown implementation is specified.
func NewProvisionerTask(cfg Config, impl string, log logger.Logger) (ProvisionerTask, error) {
	// Normalize empty string to legacy
	if impl == "" {
		impl = ImplLegacy
	}

	switch impl {
	case ImplLegacy:
		return newLegacyTask(cfg, log)
	case ImplFSM:
		return newFSMTask(cfg, log)
	default:
		return nil, errors.Errorf("unknown provisioner implementation %q (allowed: %s, %s)", impl, ImplLegacy, ImplFSM)
	}
}

// newLegacyTask creates a provisioner task using the legacy implementation.
var newLegacyTask = func(cfg Config, log logger.Logger) (ProvisionerTask, error) {
	log.Infof(context.Background(), "using legacy provisioner task implementation")
	return provisionertask.NewProvisionerTask(cfg.TaskConfig)
}

// newFSMTask creates a provisioner task using the new FSM implementation.
var newFSMTask = func(cfg Config, log logger.Logger) (ProvisionerTask, error) {
	log.Infof(context.Background(), "using FSM provisioner task implementation")

	// Convert time.Duration to int (seconds) for bis which doesn't use sleep-based retries
	retryDelay := 0
	if cfg.RetryStartInstanceStrategy.RetryDelay > 0 {
		retryDelay = int(cfg.RetryStartInstanceStrategy.RetryDelay / time.Second)
	}

	bisCfg := provisionertaskbis.LegacyTaskConfig{
		ControllerUUID:               cfg.ControllerUUID,
		HostTag:                      cfg.HostTag,
		Logger:                       cfg.Logger,
		ControllerAPI:                cfg.ControllerAPI,
		MachinesAPI:                  cfg.MachinesAPI,
		GetMachineInstanceInfoSetter: provisionertaskbis.LegacyGetMachineInstanceInfoSetter(cfg.GetMachineInstanceInfoSetter),
		DistributionGroupFinder:      cfg.DistributionGroupFinder,
		ToolsFinder:                  cfg.ToolsFinder,
		MachineWatcher:               cfg.MachineWatcher,
		RetryWatcher:                 cfg.RetryWatcher,
		Broker:                       cfg.Broker,
		ImageStream:                  cfg.ImageStream,
		RetryStartInstanceStrategy: provisionertaskbis.RetryStrategy{
			RetryDelay: retryDelay,
			RetryCount: cfg.RetryStartInstanceStrategy.RetryCount,
		},
		NumProvisionWorkers: cfg.NumProvisionWorkers,
		EventProcessedCb:    cfg.EventProcessedCb,
		AvailabilityZones:   cfg.AvailabilityZones,
	}

	return provisionertaskbis.NewProvisionerTaskFromLegacy(bisCfg)
}
