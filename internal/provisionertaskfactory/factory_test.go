// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package provisionertaskfactory

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/juju/names/v6"
	tc "github.com/juju/tc"
	"github.com/juju/worker/v4"

	corelogger "github.com/juju/juju/core/logger"
	"github.com/juju/juju/core/watcher/watchertest"
	"github.com/juju/juju/internal/provisionertask"
)

// Test suite runner
func TestFactorySuite(t *testing.T) {
	tc.Run(t, &FactorySuite{})
}

// FactorySuite contains unit tests for the provisioner task factory.
type FactorySuite struct{}

func (s *FactorySuite) TestDefaultSelectsLegacy(c *tc.C) {
	// Track which implementation was selected
	var legacyCalled, fsmCalled atomic.Bool

	// Save original functions
	origLegacy := newLegacyTask
	origFSM := newFSMTask
	defer func() {
		newLegacyTask = origLegacy
		newFSMTask = origFSM
	}()

	// Replace with tracking versions
	newLegacyTask = func(cfg Config, log corelogger.Logger) (ProvisionerTask, error) {
		legacyCalled.Store(true)
		return &fakeTask{}, nil
	}
	newFSMTask = func(cfg Config, log corelogger.Logger) (ProvisionerTask, error) {
		fsmCalled.Store(true)
		return &fakeTask{}, nil
	}

	cfg := minimalConfig(c)
	logger := &stubLogger{}

	// Test with empty string (default)
	task, err := NewProvisionerTask(cfg, "", logger)
	c.Assert(err, tc.IsNil)
	c.Assert(task, tc.NotNil)
	c.Assert(legacyCalled.Load(), tc.IsTrue)
	c.Assert(fsmCalled.Load(), tc.IsFalse)

	// Reset
	legacyCalled.Store(false)

	// Test with explicit "legacy"
	task, err = NewProvisionerTask(cfg, ImplLegacy, logger)
	c.Assert(err, tc.IsNil)
	c.Assert(task, tc.NotNil)
	c.Assert(legacyCalled.Load(), tc.IsTrue)
	c.Assert(fsmCalled.Load(), tc.IsFalse)
}

func (s *FactorySuite) TestFlagSelectsFSM(c *tc.C) {
	// Track which implementation was selected
	var legacyCalled, fsmCalled atomic.Bool

	// Save original functions
	origLegacy := newLegacyTask
	origFSM := newFSMTask
	defer func() {
		newLegacyTask = origLegacy
		newFSMTask = origFSM
	}()

	// Replace with tracking versions
	newLegacyTask = func(cfg Config, log corelogger.Logger) (ProvisionerTask, error) {
		legacyCalled.Store(true)
		return &fakeTask{}, nil
	}
	newFSMTask = func(cfg Config, log corelogger.Logger) (ProvisionerTask, error) {
		fsmCalled.Store(true)
		return &fakeTask{}, nil
	}

	cfg := minimalConfig(c)
	logger := &stubLogger{}

	// Test with "fsm"
	task, err := NewProvisionerTask(cfg, ImplFSM, logger)
	c.Assert(err, tc.IsNil)
	c.Assert(task, tc.NotNil)
	c.Assert(legacyCalled.Load(), tc.IsFalse)
	c.Assert(fsmCalled.Load(), tc.IsTrue)
}

func (s *FactorySuite) TestInvalidFlagErrors(c *tc.C) {
	cfg := minimalConfig(c)
	logger := &stubLogger{}

	// Test with invalid value
	task, err := NewProvisionerTask(cfg, "wat", logger)
	c.Assert(task, tc.IsNil)
	c.Assert(err, tc.NotNil)

	// Verify error message includes the invalid value and allowed values
	c.Assert(err.Error(), tc.Matches, `.*"wat".*`)
	c.Assert(err.Error(), tc.Matches, `.*legacy.*`)
	c.Assert(err.Error(), tc.Matches, `.*fsm.*`)
}

func (s *FactorySuite) TestEventProcessedCallbackStillFires(c *tc.C) {
	// This test verifies both implementations can receive an EventProcessedCb
	// and that it's properly wired.
	var callbackFired atomic.Bool

	// Save original functions
	origLegacy := newLegacyTask
	origFSM := newFSMTask
	defer func() {
		newLegacyTask = origLegacy
		newFSMTask = origFSM
	}()

	// Replace with versions that verify callback is configured
	newLegacyTask = func(cfg Config, log corelogger.Logger) (ProvisionerTask, error) {
		if cfg.EventProcessedCb != nil {
			cfg.EventProcessedCb("test-event")
		}
		return &fakeTask{}, nil
	}
	newFSMTask = func(cfg Config, log corelogger.Logger) (ProvisionerTask, error) {
		if cfg.EventProcessedCb != nil {
			cfg.EventProcessedCb("test-event")
		}
		return &fakeTask{}, nil
	}

	logger := &stubLogger{}

	// Test legacy
	callbackFired.Store(false)
	cfg := minimalConfig(c)
	cfg.EventProcessedCb = func(evt string) {
		callbackFired.Store(true)
	}
	_, err := NewProvisionerTask(cfg, ImplLegacy, logger)
	c.Assert(err, tc.IsNil)
	c.Assert(callbackFired.Load(), tc.IsTrue)

	// Test FSM
	callbackFired.Store(false)
	cfg = minimalConfig(c)
	cfg.EventProcessedCb = func(evt string) {
		callbackFired.Store(true)
	}
	_, err = NewProvisionerTask(cfg, ImplFSM, logger)
	c.Assert(err, tc.IsNil)
	c.Assert(callbackFired.Load(), tc.IsTrue)
}

// minimalConfig returns a minimal Config for testing.
func minimalConfig(c *tc.C) Config {
	machineChanges := make(chan []string)
	machineWatcher := watchertest.NewMockStringsWatcher(machineChanges)
	c.Cleanup(func() { close(machineChanges) })

	return Config{
		TaskConfig: provisionertask.TaskConfig{
			ControllerUUID:      "test-controller-uuid",
			HostTag:             names.NewMachineTag("0"),
			Logger:              &stubLogger{},
			NumProvisionWorkers: 10,
			MachineWatcher:      machineWatcher,
		},
	}
}

// fakeTask implements ProvisionerTask for testing.
type fakeTask struct{}

func (t *fakeTask) Kill()                        {}
func (t *fakeTask) Wait() error                  { return nil }
func (t *fakeTask) SetNumProvisionWorkers(n int) {}

// stubLogger implements logger.Logger for testing.
type stubLogger struct{}

func (l *stubLogger) Tracef(ctx context.Context, msg string, args ...any)    {}
func (l *stubLogger) Debugf(ctx context.Context, msg string, args ...any)    {}
func (l *stubLogger) Infof(ctx context.Context, msg string, args ...any)     {}
func (l *stubLogger) Warningf(ctx context.Context, msg string, args ...any)  {}
func (l *stubLogger) Errorf(ctx context.Context, msg string, args ...any)    {}
func (l *stubLogger) Criticalf(ctx context.Context, msg string, args ...any) {}
func (l *stubLogger) Logf(ctx context.Context, level corelogger.Level, labels corelogger.Labels, format string, args ...any) {
}
func (l *stubLogger) IsLevelEnabled(level corelogger.Level) bool { return false }
func (l *stubLogger) Child(name string, tags ...string) corelogger.Logger {
	return l
}
func (l *stubLogger) GetChildByName(name string) corelogger.Logger {
	return l
}
func (l *stubLogger) Helper() {}

var _ worker.Worker = (*fakeTask)(nil)
var _ ProvisionerTask = (*fakeTask)(nil)
