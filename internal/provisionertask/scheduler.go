// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package provisionertask

import (
	"github.com/juju/juju/core/logger"
)

// readyQueue holds machines ready for provider operations.
// This is a placeholder for future integration.
type readyQueue struct {
	// machines holds the list of machine IDs ready for operations.
	machines []string
}

// scheduler manages the scheduling of provider operations across
// critical and normal priority queues.
// This is a placeholder for future integration.
type scheduler struct {
	logger logger.Logger
	
	// criticalQ holds high-priority operations.
	criticalQ *readyQueue
	
	// normalQ holds normal-priority operations.
	normalQ *readyQueue
}

// newScheduler creates a new scheduler instance.
func newScheduler(logger logger.Logger) *scheduler {
	return &scheduler{
		logger:    logger,
		criticalQ: &readyQueue{machines: make([]string, 0)},
		normalQ:   &readyQueue{machines: make([]string, 0)},
	}
}

// markEligible marks a machine as eligible for scheduling.
// TODO: Future integration - currently a no-op to ensure no behaviour change.
func (s *scheduler) markEligible(machineID string) {
	// Intentionally empty - no-op for scaffolding phase
}

// submitReady submits ready operations to the provider.
// TODO: Future integration - currently a no-op to ensure no behaviour change.
func (s *scheduler) submitReady() {
	// Intentionally empty - no-op for scaffolding phase
}
