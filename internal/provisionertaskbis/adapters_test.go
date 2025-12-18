// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package provisionertaskbis

import (
	"context"
	"testing"

	"github.com/juju/names/v6"
	tc "github.com/juju/tc"

	apiprovisioner "github.com/juju/juju/api/agent/provisioner"
	"github.com/juju/juju/core/instance"
	corelogger "github.com/juju/juju/core/logger"
	"github.com/juju/juju/core/watcher"
	"github.com/juju/juju/core/watcher/watchertest"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/environs/instances"
	"github.com/juju/juju/rpc/params"
)

// Test suite runner
func TestAdaptersSuite(t *testing.T) {
	tc.Run(t, &AdaptersSuite{})
}

// AdaptersSuite contains unit tests for adapter types.
type AdaptersSuite struct{}

// Tests for loggerAdapter

func (s *AdaptersSuite) TestLoggerAdapterCallsThrough(c *tc.C) {
	// This is a simple test that the logger adapter doesn't panic
	adapter := &loggerAdapter{logger: &stubCoreLogger{}}

	ctx := context.Background()
	adapter.Tracef(ctx, "trace %d", 1)
	adapter.Debugf(ctx, "debug %d", 2)
	adapter.Infof(ctx, "info %d", 3)
	adapter.Warningf(ctx, "warning %d", 4)
	adapter.Errorf(ctx, "error %d", 5)
}

// Tests for NewProvisionerTaskFromLegacy validation

func (s *AdaptersSuite) TestNewProvisionerTaskFromLegacyMissingLogger(c *tc.C) {
	machineChanges := make(chan []string)
	machineWatcher := watchertest.NewMockStringsWatcher(machineChanges)
	defer close(machineChanges)

	cfg := LegacyTaskConfig{
		MachinesAPI:    &stubLegacyMachinesAPI{},
		Broker:         &stubLegacyBroker{},
		MachineWatcher: machineWatcher,
	}
	_, err := NewProvisionerTaskFromLegacy(cfg)
	c.Assert(err, tc.ErrorMatches, ".*nil Logger.*")
}

func (s *AdaptersSuite) TestNewProvisionerTaskFromLegacyMissingMachinesAPI(c *tc.C) {
	machineChanges := make(chan []string)
	machineWatcher := watchertest.NewMockStringsWatcher(machineChanges)
	defer close(machineChanges)

	cfg := LegacyTaskConfig{
		Logger:         &stubCoreLogger{},
		Broker:         &stubLegacyBroker{},
		MachineWatcher: machineWatcher,
	}
	_, err := NewProvisionerTaskFromLegacy(cfg)
	c.Assert(err, tc.ErrorMatches, ".*nil MachinesAPI.*")
}

func (s *AdaptersSuite) TestNewProvisionerTaskFromLegacyMissingBroker(c *tc.C) {
	machineChanges := make(chan []string)
	machineWatcher := watchertest.NewMockStringsWatcher(machineChanges)
	defer close(machineChanges)

	cfg := LegacyTaskConfig{
		Logger:         &stubCoreLogger{},
		MachinesAPI:    &stubLegacyMachinesAPI{},
		MachineWatcher: machineWatcher,
	}
	_, err := NewProvisionerTaskFromLegacy(cfg)
	c.Assert(err, tc.ErrorMatches, ".*nil Broker.*")
}

func (s *AdaptersSuite) TestNewProvisionerTaskFromLegacyMissingMachineWatcher(c *tc.C) {
	cfg := LegacyTaskConfig{
		Logger:      &stubCoreLogger{},
		MachinesAPI: &stubLegacyMachinesAPI{},
		Broker:      &stubLegacyBroker{},
	}
	_, err := NewProvisionerTaskFromLegacy(cfg)
	c.Assert(err, tc.ErrorMatches, ".*nil MachineWatcher.*")
}

// Tests for brokerAdapter

func (s *AdaptersSuite) TestBrokerAdapterStopInstancesConvertsIDs(c *tc.C) {
	stub := &stubLegacyBroker{}
	adapter := &brokerAdapter{
		broker: stub,
		logger: &fakeLogger{},
	}

	ctx := context.Background()
	err := adapter.StopInstances(ctx, "i-1", "i-2", "i-3")
	c.Assert(err, tc.IsNil)

	// Verify the broker received the converted IDs
	c.Assert(stub.stopInstancesCalls, tc.HasLen, 1)
	c.Assert(stub.stopInstancesCalls[0], tc.DeepEquals, []instance.Id{"i-1", "i-2", "i-3"})
}

// Stub implementations for testing

// stubCoreLogger implements corelogger.Logger for testing.
type stubCoreLogger struct{}

func (l *stubCoreLogger) Tracef(ctx context.Context, msg string, args ...any)    {}
func (l *stubCoreLogger) Debugf(ctx context.Context, msg string, args ...any)    {}
func (l *stubCoreLogger) Infof(ctx context.Context, msg string, args ...any)     {}
func (l *stubCoreLogger) Warningf(ctx context.Context, msg string, args ...any)  {}
func (l *stubCoreLogger) Errorf(ctx context.Context, msg string, args ...any)    {}
func (l *stubCoreLogger) Criticalf(ctx context.Context, msg string, args ...any) {}
func (l *stubCoreLogger) Logf(ctx context.Context, level corelogger.Level, labels corelogger.Labels, format string, args ...any) {
}
func (l *stubCoreLogger) IsLevelEnabled(level corelogger.Level) bool { return false }
func (l *stubCoreLogger) Child(name string, tags ...string) corelogger.Logger {
	return l
}
func (l *stubCoreLogger) GetChildByName(name string) corelogger.Logger {
	return l
}
func (l *stubCoreLogger) Helper() {}

// stubLegacyMachinesAPI implements LegacyMachinesAPI for testing.
type stubLegacyMachinesAPI struct{}

func (a *stubLegacyMachinesAPI) Machines(ctx context.Context, tags ...names.MachineTag) ([]apiprovisioner.MachineResult, error) {
	return nil, nil
}

func (a *stubLegacyMachinesAPI) MachinesWithTransientErrors(ctx context.Context) ([]apiprovisioner.MachineStatusResult, error) {
	return nil, nil
}

func (a *stubLegacyMachinesAPI) WatchMachineErrorRetry(ctx context.Context) (watcher.NotifyWatcher, error) {
	return nil, nil
}

func (a *stubLegacyMachinesAPI) WatchModelMachines(ctx context.Context) (watcher.StringsWatcher, error) {
	return nil, nil
}

func (a *stubLegacyMachinesAPI) ProvisioningInfo(ctx context.Context, machineTags []names.MachineTag) (params.ProvisioningInfoResults, error) {
	return params.ProvisioningInfoResults{}, nil
}

// stubLegacyBroker implements environs.InstanceBroker for testing.
type stubLegacyBroker struct {
	stopInstancesCalls [][]instance.Id
}

func (b *stubLegacyBroker) StartInstance(ctx context.Context, args environs.StartInstanceParams) (*environs.StartInstanceResult, error) {
	return nil, nil
}

func (b *stubLegacyBroker) StopInstances(ctx context.Context, ids ...instance.Id) error {
	b.stopInstancesCalls = append(b.stopInstancesCalls, ids)
	return nil
}

func (b *stubLegacyBroker) AllInstances(ctx context.Context) ([]instances.Instance, error) {
	return nil, nil
}

func (b *stubLegacyBroker) AllRunningInstances(ctx context.Context) ([]instances.Instance, error) {
	return nil, nil
}
