// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package provisionertask

import (
	"github.com/juju/juju/core/logger"
)

// readyQueue holds machines ready for provider operations.
// It maintains two priority queues: critical and normal.
type readyQueue struct {
	// CriticalQ holds high-priority operations (reserved for future use).
	CriticalQ []*MachineCtx
	// NormalQ holds normal-priority operations (reserved for future use).
	NormalQ []*MachineCtx
}

// scheduler manages the scheduling of provider operations for machines.
// This is scaffolding for future integration; methods are currently no-ops.
type scheduler struct {
	logger logger.Logger
	ready  *readyQueue
}

// newScheduler creates a new scheduler instance.
func newScheduler(logger logger.Logger) *scheduler {
	return &scheduler{
		logger: logger,
		ready: &readyQueue{
			CriticalQ: make([]*MachineCtx, 0),
			NormalQ:   make([]*MachineCtx, 0),
		},
	}
}

// markEligible marks a machine as eligible for scheduling.
// TODO: Future integration will add machines to the ready queue based on state.
// Currently a no-op to ensure no behaviour change.
func (s *scheduler) markEligible(mc *MachineCtx) {
	// Intentionally empty - reserved for future use.
	// Will add mc to appropriate queue (CriticalQ or NormalQ) based on priority.
}

// submitReady submits ready operations to the provider.
// TODO: Future integration will submit queued operations for execution.
// Currently a no-op to ensure no behaviour change.
func (s *scheduler) submitReady() {
	// Intentionally empty - reserved for future use.
	// Will process ready queue and submit provider operations.
}
