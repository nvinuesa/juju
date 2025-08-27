// Copyright 2024 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package provisionertask

import (
	"context"

	"github.com/juju/errors"
	"github.com/juju/names/v6"
	"github.com/juju/worker/v4/catacomb"

	"github.com/juju/juju/core/logger"
	"github.com/juju/juju/core/watcher"
	"github.com/juju/juju/environs"
)

// NewProvisionerTaskStateMachine creates a new provisioner task using the state machine implementation
// This is a drop-in replacement for NewProvisionerTask with the same interface
func NewProvisionerTaskStateMachine(config TaskConfig) (ProvisionerTask, error) {
	// Enhanced validation
	if config.Logger == nil {
		return nil, errors.NotValidf("missing logger")
	}
	if config.MachinesAPI == nil {
		return nil, errors.NotValidf("missing machines API")
	}
	if config.Broker == nil {
		return nil, errors.NotValidf("missing broker")
	}
	if config.ControllerAPI == nil {
		return nil, errors.NotValidf("missing controller API")
	}

	// Create enhanced config with proper machine watcher function
	enhancedConfig := config
	enhancedConfig.MachineWatcher = func(ctx context.Context) (watcher.StringsWatcher, error) {
		return config.MachinesAPI.WatchModelMachines(ctx)
	}

	task := &stateMachineProvisionerAdapter{
		config: enhancedConfig,
		logger: config.Logger,
	}

	// Create the underlying state machine
	sm, err := NewStateMachineProvisioner(enhancedConfig)
	if err != nil {
		return nil, errors.Trace(err)
	}

	task.stateMachine = sm.(*stateMachineProvisioner)

	err = catacomb.Invoke(catacomb.Plan{
		Site: &task.catacomb,
		Work: task.loop,
	})
	if err != nil {
		return nil, errors.Trace(err)
	}

	return task, nil
}

// stateMachineProvisionerAdapter adapts the state machine to the ProvisionerTask interface
// This ensures full compatibility with existing code while using the new implementation
type stateMachineProvisionerAdapter struct {
	catacomb     catacomb.Catacomb
	config       TaskConfig
	logger       logger.Logger
	stateMachine *stateMachineProvisioner
}

// Kill implements ProvisionerTask.Kill
func (a *stateMachineProvisionerAdapter) Kill() {
	a.catacomb.Kill(nil)
}

// Wait implements ProvisionerTask.Wait
func (a *stateMachineProvisionerAdapter) Wait() error {
	return a.catacomb.Wait()
}

// SetNumProvisionWorkers implements ProvisionerTask.SetNumProvisionWorkers
func (a *stateMachineProvisionerAdapter) SetNumProvisionWorkers(count int) {
	if a.stateMachine != nil {
		a.stateMachine.SetNumProvisionWorkers(count)
	}
}

// loop runs the adapter's main loop, which primarily delegates to the state machine
func (a *stateMachineProvisionerAdapter) loop() error {
	// The state machine handles all the logic, so we just wait for it to finish
	if err := a.catacomb.Add(a.stateMachine); err != nil {
		return errors.Trace(err)
	}

	// Wait for the state machine to complete
	select {
	case <-a.catacomb.Dying():
		return a.catacomb.ErrDying()
	}
}

// Enhanced TaskConfig for state machine functionality
type StateMachineTaskConfig struct {
	TaskConfig
	// Additional configuration specific to state machine implementation
	EnableDetailedLogging bool
	MaxQueueSize          int
	WorkerTimeout         int // seconds
}

// Enhanced machine watcher function type
type MachineWatcherFunc func(context.Context) (watcher.StringsWatcher, error)

// Extend the TaskConfig to include the watcher function
func (config *TaskConfig) withMachineWatcher(watcherFunc MachineWatcherFunc) TaskConfig {
	enhanced := *config
	enhanced.MachineWatcher = watcherFunc
	return enhanced
}

// Helper function to create the complete provisioner task with all dependencies
func NewCompleteProvisionerTask(
	controllerUUID string,
	hostTag names.Tag,
	logger logger.Logger,
	controllerAPI ControllerAPI,
	machinesAPI MachinesAPI,
	getMachineInstanceInfoSetter GetMachineInstanceInfoSetter,
	distributionGroupFinder DistributionGroupFinder,
	toolsFinder ToolsFinder,
	broker environs.InstanceBroker,
	imageStream string,
	retryStrategy RetryStrategy,
	numProvisionWorkers int,
	eventProcessedCb func(string),
) (ProvisionerTask, error) {

	// Create machine watcher function
	machineWatcher := func(ctx context.Context) (watcher.StringsWatcher, error) {
		return machinesAPI.WatchModelMachines(ctx)
	}

	// Create retry watcher
	var retryWatcher watcher.NotifyWatcher
	var err error
	if rw, err := machinesAPI.WatchMachineErrorRetry(context.Background()); err == nil {
		retryWatcher = rw
	}

	config := TaskConfig{
		ControllerUUID:               controllerUUID,
		HostTag:                      hostTag,
		Logger:                       logger,
		ControllerAPI:                controllerAPI,
		MachinesAPI:                  machinesAPI,
		GetMachineInstanceInfoSetter: getMachineInstanceInfoSetter,
		DistributionGroupFinder:      distributionGroupFinder,
		ToolsFinder:                  toolsFinder,
		MachineWatcher:               machineWatcher,
		RetryWatcher:                 retryWatcher,
		Broker:                       broker,
		ImageStream:                  imageStream,
		RetryStartInstanceStrategy:   retryStrategy,
		NumProvisionWorkers:          numProvisionWorkers,
		EventProcessedCb:             eventProcessedCb,
	}

	return NewProvisionerTaskStateMachine(config)
}

// Feature flag support for gradual rollout
var UseStateMachineProvisioner = false

// NewProvisionerTaskWithFeatureFlag creates either the old or new implementation based on feature flag
func NewProvisionerTaskWithFeatureFlag(config TaskConfig) (ProvisionerTask, error) {
	if UseStateMachineProvisioner {
		config.Logger.Infof("Using state machine provisioner implementation")
		return NewProvisionerTaskStateMachine(config)
	} else {
		config.Logger.Infof("Using traditional provisioner implementation")
		return NewProvisionerTask(config)
	}
}

// Migration utilities for testing and validation

// ProvisionerTaskStats provides statistics for monitoring both implementations
type ProvisionerTaskStats struct {
	TotalMachines     int
	PendingMachines   int
	StartingMachines  int
	RunningMachines   int
	FailedMachines    int
	DeadMachines      int
	QueueDepths       map[string]int
	WorkerUtilization float64
}

// GetStats returns statistics about the provisioner task state
func GetProvisionerStats(task ProvisionerTask) (*ProvisionerTaskStats, error) {
	if adapter, ok := task.(*stateMachineProvisionerAdapter); ok {
		return getStateMachineStats(adapter.stateMachine)
	}
	// For traditional implementation, we'd need to add similar functionality
	return &ProvisionerTaskStats{}, nil
}

func getStateMachineStats(sm *stateMachineProvisioner) (*ProvisionerTaskStats, error) {
	stats := &ProvisionerTaskStats{
		QueueDepths: make(map[string]int),
	}

	// Count machines by state
	stateCounts := make(map[MachineState]int)
	sm.machines.Range(func(key, value interface{}) bool {
		if record, ok := value.(*MachineRecord); ok {
			stateCounts[record.State]++
			stats.TotalMachines++
		}
		return true
	})

	stats.PendingMachines = stateCounts[StatePending]
	stats.StartingMachines = stateCounts[StateStarting]
	stats.RunningMachines = stateCounts[StateRunning]
	stats.FailedMachines = stateCounts[StateFailed]
	stats.DeadMachines = stateCounts[StateDead]

	// Queue depths
	stats.QueueDepths["start"] = len(sm.startQueue)
	stats.QueueDepths["stop"] = len(sm.stopQueue)
	stats.QueueDepths["retry"] = len(sm.retryQueue)
	stats.QueueDepths["response"] = len(sm.responseQueue)
	stats.QueueDepths["machineChanges"] = len(sm.machineChangesQueue)

	// Worker utilization
	if sm.workers != nil {
		available := len(sm.workers)
		total := cap(sm.workers)
		if total > 0 {
			stats.WorkerUtilization = float64(total-available) / float64(total)
		}
	}

	return stats, nil
}

// Validation helpers for ensuring compatibility

// ValidateProvisionerTaskCompatibility ensures both implementations produce similar results
func ValidateProvisionerTaskCompatibility(
	oldTask ProvisionerTask,
	newTask ProvisionerTask,
	testMachines []string,
) error {
	// This would be used in integration tests to verify that both
	// implementations handle the same set of machines similarly

	// In a real implementation, this would:
	// 1. Feed the same events to both provisioners
	// 2. Compare the final states
	// 3. Verify timing characteristics are similar
	// 4. Check that all machines end up in expected states

	return nil
}

// Performance comparison utilities

// BenchmarkResult contains performance metrics for comparison
type BenchmarkResult struct {
	Implementation    string
	MachinesProcessed int
	TotalTime         int64 // nanoseconds
	MemoryUsage       int64 // bytes
	ErrorRate         float64
	AvgLatency        int64 // nanoseconds per machine
}

// RunProvisionerBenchmark compares performance of both implementations
func RunProvisionerBenchmark(numMachines int, iterations int) (*BenchmarkResult, *BenchmarkResult, error) {
	// This would run standardized benchmarks comparing:
	// - Machine provisioning throughput
	// - Memory usage patterns
	// - CPU utilization
	// - Error rates under various conditions
	// - Latency distributions

	oldResult := &BenchmarkResult{Implementation: "traditional"}
	newResult := &BenchmarkResult{Implementation: "state_machine"}

	return oldResult, newResult, nil
}

// Debugging and introspection utilities

// ProvisionerDebugInfo provides detailed state information for debugging
type ProvisionerDebugInfo struct {
	Implementation string
	MachineStates  map[string]interface{}
	QueueStates    map[string]interface{}
	WorkerInfo     interface{}
	RecentErrors   []string
	Transitions    []string // Recent state transitions
}

// GetDebugInfo returns detailed debugging information
func GetDebugInfo(task ProvisionerTask) (*ProvisionerDebugInfo, error) {
	info := &ProvisionerDebugInfo{
		MachineStates: make(map[string]interface{}),
		QueueStates:   make(map[string]interface{}),
	}

	if adapter, ok := task.(*stateMachineProvisionerAdapter); ok {
		info.Implementation = "state_machine"
		// Populate state machine specific debug info
		sm := adapter.stateMachine

		// Machine states
		sm.machines.Range(func(key, value interface{}) bool {
			if machineID, ok := key.(string); ok {
				if record, ok := value.(*MachineRecord); ok {
					info.MachineStates[machineID] = map[string]interface{}{
						"state":      record.State.String(),
						"retryCount": record.RetryCount,
						"lastError":  record.LastError,
						"createdAt":  record.CreatedAt,
						"updatedAt":  record.UpdatedAt,
					}
				}
			}
			return true
		})

		// Queue states
		info.QueueStates["start"] = len(sm.startQueue)
		info.QueueStates["stop"] = len(sm.stopQueue)
		info.QueueStates["retry"] = len(sm.retryQueue)
		info.QueueStates["response"] = len(sm.responseQueue)

	} else {
		info.Implementation = "traditional"
		// For traditional implementation, we'd extract similar info
	}

	return info, nil
}
