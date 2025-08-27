// Copyright 2024 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package provisionertask_test

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/juju/errors"
	"github.com/juju/names/v6"
	jc "github.com/juju/testing/checkers"
	"github.com/juju/version"
	"github.com/juju/worker/v4"
	gc "gopkg.in/check.v1"

	apiprovisioner "github.com/juju/juju/api/agent/provisioner"
	controller "github.com/juju/juju/controller"
	"github.com/juju/juju/core/constraints"
	"github.com/juju/juju/core/instance"
	"github.com/juju/juju/core/logger"
	"github.com/juju/juju/core/network"
	"github.com/juju/juju/core/semversion"
	"github.com/juju/juju/core/status"
	"github.com/juju/juju/core/watcher"
	"github.com/juju/juju/environs"
	config "github.com/juju/juju/environs/config"
	"github.com/juju/juju/environs/instances"
	"github.com/juju/juju/internal/provisionertask"
	coretools "github.com/juju/juju/internal/tools"
	"github.com/juju/juju/rpc/params"
	coretesting "github.com/juju/juju/testing"
)

type StateMachineProvisionerSuite struct {
	coretesting.BaseSuite

	// Test doubles
	controllerAPI           *mockControllerAPI
	machinesAPI             *mockMachinesAPI
	broker                  *mockBroker
	toolsFinder             *mockToolsFinder
	distributionGroupFinder *mockDistributionGroupFinder
	machineWatcher          *mockStringsWatcher
	retryWatcher            *mockNotifyWatcher
	logger                  logger.Logger

	// Test state
	machineResults     []apiprovisioner.MachineResult
	provisioningInfo   params.ProvisioningInfoResults
	instances          map[instance.Id]*mockInstance
	eventCallbacks     []string
	eventCallbackMutex sync.Mutex
}

var _ = gc.Suite(&StateMachineProvisionerSuite{})

func (s *StateMachineProvisionerSuite) SetUpTest(c *gc.C) {
	s.BaseSuite.SetUpTest(c)

	s.controllerAPI = &mockControllerAPI{}
	s.machinesAPI = &mockMachinesAPI{}
	s.broker = &mockBroker{}
	s.toolsFinder = &mockToolsFinder{}
	s.distributionGroupFinder = &mockDistributionGroupFinder{}
	s.machineWatcher = &mockStringsWatcher{
		changes: make(chan []string, 10),
	}
	s.retryWatcher = &mockNotifyWatcher{
		changes: make(chan struct{}, 10),
	}
	s.logger = &testLogger{c: c}
	s.instances = make(map[instance.Id]*mockInstance)
	s.eventCallbacks = []string{}

	// Setup default responses
	s.setupDefaultMocks()
}

func (s *StateMachineProvisionerSuite) setupDefaultMocks() {
	// Controller API defaults
	s.controllerAPI.controllerConfig = map[string]interface{}{
		"controller-uuid": "test-controller-uuid",
	}
	s.controllerAPI.modelConfig = map[string]interface{}{
		"uuid":                  "test-model-uuid",
		"name":                  "test-model",
		"type":                  "test",
		"image-stream":          "released",
		"num-provision-workers": 2,
	}
	s.controllerAPI.caCert = "test-ca-cert"
	s.controllerAPI.apiAddresses = []string{"10.0.0.1:17070"}

	// Machines API defaults
	s.machinesAPI.machineResults = []apiprovisioner.MachineResult{}
	s.machinesAPI.provisioningInfo = params.ProvisioningInfoResults{
		Results: []params.ProvisioningInfoResult{},
	}

	// Broker defaults
	s.broker.instances = s.instances
}

func (s *StateMachineProvisionerSuite) eventProcessedCallback(eventType string) {
	s.eventCallbackMutex.Lock()
	s.eventCallbacks = append(s.eventCallbacks, eventType)
	s.eventCallbackMutex.Unlock()
}

func (s *StateMachineProvisionerSuite) newProvisionerTask() (provisionertask.ProvisionerTask, error) {
	config := provisionertask.TaskConfig{
		ControllerUUID:               "test-controller-uuid",
		HostTag:                      names.NewMachineTag("0"),
		Logger:                       s.logger,
		ControllerAPI:                s.controllerAPI,
		MachinesAPI:                  s.machinesAPI,
		GetMachineInstanceInfoSetter: s.machineInstanceInfoSetter,
		DistributionGroupFinder:      s.distributionGroupFinder,
		ToolsFinder:                  s.toolsFinder,
		MachineWatcher: func(ctx context.Context) (watcher.StringsWatcher, error) {
			return s.machineWatcher, nil
		},
		RetryWatcher: s.retryWatcher,
		Broker:       s.broker,
		ImageStream:  "released",
		RetryStartInstanceStrategy: provisionertask.RetryStrategy{
			RetryDelay: 1 * time.Second,
			RetryCount: 3,
		},
		NumProvisionWorkers: 2,
		EventProcessedCb:    s.eventProcessedCallback,
	}

	return provisionertask.NewStateMachineProvisioner(config)
}

func (s *StateMachineProvisionerSuite) machineInstanceInfoSetter(machineProvisioner apiprovisioner.MachineProvisioner) func(
	ctx context.Context,
	id instance.Id, displayName string, nonce string, hc *instance.HardwareCharacteristics,
	networkConfig []params.NetworkConfig, volumes []params.Volume,
	volumeAttachments map[string]params.VolumeAttachmentInfo, charmProfiles []string,
) error {
	return func(
		ctx context.Context,
		id instance.Id, displayName string, nonce string, hc *instance.HardwareCharacteristics,
		networkConfig []params.NetworkConfig, volumes []params.Volume,
		volumeAttachments map[string]params.VolumeAttachmentInfo, charmProfiles []string,
	) error {
		// Mock implementation - just record that it was called
		return nil
	}
}

func (s *StateMachineProvisionerSuite) TestStartStop(c *gc.C) {
	task, err := s.newProvisionerTask()
	c.Assert(err, jc.ErrorIsNil)

	// Start the task
	c.Assert(task, gc.NotNil)

	// Stop the task
	task.Kill()
	err = task.Wait()
	c.Assert(err, jc.ErrorIsNil)
}

func (s *StateMachineProvisionerSuite) TestSetNumProvisionWorkers(c *gc.C) {
	task, err := s.newProvisionerTask()
	c.Assert(err, jc.ErrorIsNil)
	defer func() {
		task.Kill()
		_ = task.Wait()
	}()

	// Change worker count
	task.SetNumProvisionWorkers(5)

	// Wait a bit for the change to be processed
	time.Sleep(100 * time.Millisecond)

	// Check that event was processed
	s.eventCallbackMutex.Lock()
	found := false
	for _, event := range s.eventCallbacks {
		if event == "eventTypeResizedWorkerPool" {
			found = true
			break
		}
	}
	s.eventCallbackMutex.Unlock()
	c.Assert(found, jc.IsTrue)
}

func (s *StateMachineProvisionerSuite) TestSingleMachineProvisioning(c *gc.C) {
	// Setup a machine that needs provisioning
	machine := &mockMachineProvisioner{
		id:         "1",
		life:       params.Alive,
		status:     params.StatusPending,
		instanceId: "",
	}

	s.machinesAPI.machineResults = []apiprovisioner.MachineResult{
		{Machine: machine, Err: nil},
	}

	// Setup provisioning info
	tools := &coretools.Tools{
		Version: version.MustParseBinary("2.9.0-ubuntu-amd64"),
		URL:     "http://example.com/tools",
	}

	s.machinesAPI.provisioningInfo = params.ProvisioningInfoResults{
		Results: []params.ProvisioningInfoResult{
			{
				Result: &params.ProvisioningInfo{
					Base:          params.Base{Name: "ubuntu", Channel: "20.04/stable"},
					Tools:         tools,
					Constraints:   constraints.Value{},
					NetworkConfig: []params.NetworkConfig{},
				},
			},
		},
	}

	// Setup broker to succeed
	s.broker.startInstanceResult = &environs.StartInstanceResult{
		Instance:    &mockInstance{id: "inst-1"},
		DisplayName: "test-instance",
		Hardware:    &instance.HardwareCharacteristics{},
		NetworkInfo: &instance.NetworkInfo{},
	}

	task, err := s.newProvisionerTask()
	c.Assert(err, jc.ErrorIsNil)
	defer func() {
		task.Kill()
		_ = task.Wait()
	}()

	// Trigger machine change
	s.machineWatcher.changes <- []string{"1"}

	// Wait for processing
	s.waitForEventProcessed(c, "eventTypeProcessedMachines", 2*time.Second)

	// Verify instance was created
	c.Assert(len(s.instances), gc.Equals, 1)
}

func (s *StateMachineProvisionerSuite) TestMachineProvisioningFailureAndRetry(c *gc.C) {
	// Setup a machine that needs provisioning
	machine := &mockMachineProvisioner{
		id:         "1",
		life:       params.Alive,
		status:     params.StatusPending,
		instanceId: "",
	}

	s.machinesAPI.machineResults = []apiprovisioner.MachineResult{
		{Machine: machine, Err: nil},
	}

	// Setup provisioning info
	tools := &coretools.Tools{
		Version: version.MustParseBinary("2.9.0-ubuntu-amd64"),
		URL:     "http://example.com/tools",
	}

	s.machinesAPI.provisioningInfo = params.ProvisioningInfoResults{
		Results: []params.ProvisioningInfoResult{
			{
				Result: &params.ProvisioningInfo{
					Base:          params.Base{Name: "ubuntu", Channel: "20.04/stable"},
					Tools:         tools,
					Constraints:   constraints.Value{},
					NetworkConfig: []params.NetworkConfig{},
				},
			},
		},
	}

	// Setup broker to fail first time, succeed second time
	failCount := 0
	s.broker.startInstanceFunc = func(ctx context.Context, params environs.StartInstanceParams) (*environs.StartInstanceResult, error) {
		failCount++
		if failCount == 1 {
			return nil, errors.New("provisioning failed")
		}
		return &environs.StartInstanceResult{
			Instance:    &mockInstance{id: "inst-1"},
			DisplayName: "test-instance",
			Hardware:    &instance.HardwareCharacteristics{},
			NetworkInfo: &instance.NetworkInfo{},
		}, nil
	}

	task, err := s.newProvisionerTask()
	c.Assert(err, jc.ErrorIsNil)
	defer func() {
		task.Kill()
		_ = task.Wait()
	}()

	// Trigger machine change (first attempt will fail)
	s.machineWatcher.changes <- []string{"1"}
	s.waitForEventProcessed(c, "eventTypeProcessedMachines", 2*time.Second)

	// Trigger retry
	s.retryWatcher.changes <- struct{}{}
	s.waitForEventProcessed(c, "eventTypeRetriedMachinesWithErrors", 2*time.Second)

	// Verify instance was eventually created
	c.Assert(len(s.instances), gc.Equals, 1)
	c.Assert(failCount, gc.Equals, 2)
}

func (s *StateMachineProvisionerSuite) TestMachineStoppingBeforeStartComplete(c *gc.C) {
	// Setup a machine that needs provisioning
	machine := &mockMachineProvisioner{
		id:         "1",
		life:       params.Alive,
		status:     params.StatusPending,
		instanceId: "",
	}

	s.machinesAPI.machineResults = []apiprovisioner.MachineResult{
		{Machine: machine, Err: nil},
	}

	// Setup provisioning info
	tools := &coretools.Tools{
		Version: version.MustParseBinary("2.9.0-ubuntu-amd64"),
		URL:     "http://example.com/tools",
	}

	s.machinesAPI.provisioningInfo = params.ProvisioningInfoResults{
		Results: []params.ProvisioningInfoResult{
			{
				Result: &params.ProvisioningInfo{
					Base:          params.Base{Name: "ubuntu", Channel: "20.04/stable"},
					Tools:         tools,
					Constraints:   constraints.Value{},
					NetworkConfig: []params.NetworkConfig{},
				},
			},
		},
	}

	// Setup broker with slow start (to allow stop during start)
	s.broker.startInstanceFunc = func(ctx context.Context, params environs.StartInstanceParams) (*environs.StartInstanceResult, error) {
		time.Sleep(500 * time.Millisecond) // Slow start
		return &environs.StartInstanceResult{
			Instance:    &mockInstance{id: "inst-1"},
			DisplayName: "test-instance",
			Hardware:    &instance.HardwareCharacteristics{},
			NetworkInfo: &instance.NetworkInfo{},
		}, nil
	}

	task, err := s.newProvisionerTask()
	c.Assert(err, jc.ErrorIsNil)
	defer func() {
		task.Kill()
		_ = task.Wait()
	}()

	// Start provisioning
	s.machineWatcher.changes <- []string{"1"}

	// Quickly change to dying state (stop request)
	time.Sleep(100 * time.Millisecond)
	machine.life = params.Dying
	s.machineWatcher.changes <- []string{"1"}

	// Wait for processing
	time.Sleep(1 * time.Second)

	// The state machine should handle this gracefully:
	// - Machine should be in StopDeferred state initially
	// - After start completes, it should transition to Stopping
	// - Finally transition to Dead
}

func (s *StateMachineProvisionerSuite) TestMultipleMachinesProvisioning(c *gc.C) {
	// Setup multiple machines
	machine1 := &mockMachineProvisioner{id: "1", life: params.Alive, status: params.StatusPending}
	machine2 := &mockMachineProvisioner{id: "2", life: params.Alive, status: params.StatusPending}
	machine3 := &mockMachineProvisioner{id: "3", life: params.Alive, status: params.StatusPending}

	s.machinesAPI.machineResults = []apiprovisioner.MachineResult{
		{Machine: machine1, Err: nil},
		{Machine: machine2, Err: nil},
		{Machine: machine3, Err: nil},
	}

	// Setup provisioning info
	tools := &coretools.Tools{
		Version: version.MustParseBinary("2.9.0-ubuntu-amd64"),
		URL:     "http://example.com/tools",
	}

	s.machinesAPI.provisioningInfo = params.ProvisioningInfoResults{
		Results: []params.ProvisioningInfoResult{
			{
				Result: &params.ProvisioningInfo{
					Base:          params.Base{Name: "ubuntu", Channel: "20.04/stable"},
					Tools:         tools,
					Constraints:   constraints.Value{},
					NetworkConfig: []params.NetworkConfig{},
				},
			},
		},
	}

	// Setup broker to succeed
	instanceCount := 0
	s.broker.startInstanceFunc = func(ctx context.Context, params environs.StartInstanceParams) (*environs.StartInstanceResult, error) {
		instanceCount++
		return &environs.StartInstanceResult{
			Instance:    &mockInstance{id: instance.Id(fmt.Sprintf("inst-%d", instanceCount))},
			DisplayName: fmt.Sprintf("test-instance-%d", instanceCount),
			Hardware:    &instance.HardwareCharacteristics{},
			NetworkInfo: &instance.NetworkInfo{},
		}, nil
	}

	task, err := s.newProvisionerTask()
	c.Assert(err, jc.ErrorIsNil)
	defer func() {
		task.Kill()
		_ = task.Wait()
	}()

	// Trigger multiple machine changes
	s.machineWatcher.changes <- []string{"1", "2", "3"}

	// Wait for processing - should handle all 3 machines
	s.waitForEventProcessed(c, "eventTypeProcessedMachines", 5*time.Second)

	// Give time for all provisions to complete
	time.Sleep(2 * time.Second)

	// Verify all instances were created
	c.Assert(len(s.instances), gc.Equals, 3)
	c.Assert(instanceCount, gc.Equals, 3)
}

// Utility methods
func (s *StateMachineProvisionerSuite) waitForEventProcessed(c *gc.C, eventType string, timeout time.Duration) {
	timeoutCh := time.After(timeout)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeoutCh:
			c.Fatalf("timeout waiting for event %s", eventType)
		case <-ticker.C:
			s.eventCallbackMutex.Lock()
			found := false
			for _, event := range s.eventCallbacks {
				if event == eventType {
					found = true
					break
				}
			}
			s.eventCallbackMutex.Unlock()
			if found {
				return
			}
		}
	}
}

// Mock implementations

type mockControllerAPI struct {
	controllerConfig map[string]interface{}
	modelConfig      map[string]interface{}
	caCert           string
	apiAddresses     []string
}

func (m *mockControllerAPI) ControllerConfig(context.Context) (controller.Config, error) {
	return controller.Config(m.controllerConfig), nil
}

func (m *mockControllerAPI) CACert(context.Context) (string, error) {
	return m.caCert, nil
}

func (m *mockControllerAPI) ModelUUID(context.Context) (string, error) {
	return m.modelConfig["uuid"].(string), nil
}

func (m *mockControllerAPI) ModelConfig(context.Context) (*config.Config, error) {
	cfg, err := config.New(config.UseDefaults, m.modelConfig)
	return cfg, err
}

func (m *mockControllerAPI) WatchForModelConfigChanges(context.Context) (watcher.NotifyWatcher, error) {
	return &mockNotifyWatcher{changes: make(chan struct{})}, nil
}

func (m *mockControllerAPI) APIAddresses(context.Context) ([]string, error) {
	return m.apiAddresses, nil
}

type mockMachinesAPI struct {
	machineResults     []apiprovisioner.MachineResult
	provisioningInfo   params.ProvisioningInfoResults
	machinesWithErrors []apiprovisioner.MachineStatusResult
}

func (m *mockMachinesAPI) Machines(ctx context.Context, tags ...names.MachineTag) ([]apiprovisioner.MachineResult, error) {
	return m.machineResults, nil
}

func (m *mockMachinesAPI) MachinesWithTransientErrors(ctx context.Context) ([]apiprovisioner.MachineStatusResult, error) {
	return m.machinesWithErrors, nil
}

func (m *mockMachinesAPI) WatchMachineErrorRetry(ctx context.Context) (watcher.NotifyWatcher, error) {
	return &mockNotifyWatcher{changes: make(chan struct{})}, nil
}

func (m *mockMachinesAPI) WatchModelMachines(ctx context.Context) (watcher.StringsWatcher, error) {
	return &mockStringsWatcher{changes: make(chan []string)}, nil
}

func (m *mockMachinesAPI) ProvisioningInfo(ctx context.Context, machineTags []names.MachineTag) (params.ProvisioningInfoResults, error) {
	return m.provisioningInfo, nil
}

type mockBroker struct {
	instances           map[instance.Id]*mockInstance
	startInstanceResult *environs.StartInstanceResult
	startInstanceFunc   func(ctx context.Context, params environs.StartInstanceParams) (*environs.StartInstanceResult, error)
}

func (m *mockBroker) StartInstance(ctx context.Context, params environs.StartInstanceParams) (*environs.StartInstanceResult, error) {
	if m.startInstanceFunc != nil {
		result, err := m.startInstanceFunc(ctx, params)
		if err == nil && result != nil {
			if mockInst, ok := result.Instance.(*mockInstance); ok {
				m.instances[mockInst.id] = mockInst
			}
		}
		return result, err
	}

	if m.startInstanceResult != nil {
		if mockInst, ok := m.startInstanceResult.Instance.(*mockInstance); ok {
			m.instances[mockInst.id] = mockInst
		}
		return m.startInstanceResult, nil
	}

	return nil, errors.New("no start instance configuration")
}

func (m *mockBroker) StopInstances(ctx context.Context, ids ...instance.Id) error {
	for _, id := range ids {
		delete(m.instances, id)
	}
	return nil
}

func (m *mockBroker) AllInstances(ctx context.Context) ([]instances.Instance, error) {
	var result []instances.Instance
	for _, inst := range m.instances {
		result = append(result, inst)
	}
	return result, nil
}

func (m *mockBroker) AllRunningInstances(ctx context.Context) ([]instances.Instance, error) {
	return m.AllInstances(ctx)
}

type mockInstance struct {
	id instance.Id
}

func (m *mockInstance) Id() instance.Id {
	return m.id
}

func (m *mockInstance) Status(ctx context.Context) instance.Status {
	return instance.Status{Status: status.Running}
}

func (m *mockInstance) Addresses(ctx context.Context) (network.ProviderAddresses, error) {
	return nil, nil
}

type mockMachineProvisioner struct {
	id         string
	life       params.Life
	status     params.Status
	instanceId instance.Id
}

func (m *mockMachineProvisioner) Id() string        { return m.id }
func (m *mockMachineProvisioner) Life() params.Life { return m.life }
func (m *mockMachineProvisioner) Status() (params.StatusResult, error) {
	return params.StatusResult{Status: m.status}, nil
}
func (m *mockMachineProvisioner) InstanceId() (instance.Id, error) {
	if m.instanceId == "" {
		return "", errors.NotProvisionedf("machine %s", m.id)
	}
	return m.instanceId, nil
}
func (m *mockMachineProvisioner) Tag() names.Tag               { return names.NewMachineTag(m.id) }
func (m *mockMachineProvisioner) MachineTag() names.MachineTag { return names.NewMachineTag(m.id) }
func (m *mockMachineProvisioner) ContainerType() string        { return "" }
func (m *mockMachineProvisioner) SetStatus(ctx context.Context, status status.Status, info string, data map[string]interface{}) error {
	return nil
}

type mockStringsWatcher struct {
	worker.Worker
	changes chan []string
}

func (m *mockStringsWatcher) Changes() <-chan []string { return m.changes }
func (m *mockStringsWatcher) Kill()                    {}
func (m *mockStringsWatcher) Wait() error              { return nil }

type mockNotifyWatcher struct {
	worker.Worker
	changes chan struct{}
}

func (m *mockNotifyWatcher) Changes() <-chan struct{} { return m.changes }
func (m *mockNotifyWatcher) Kill()                    {}
func (m *mockNotifyWatcher) Wait() error              { return nil }

type mockToolsFinder struct{}

func (m *mockToolsFinder) FindTools(ctx context.Context, version semversion.Number, os string, arch string) (coretools.List, error) {
	return coretools.List{
		&coretools.Tools{
			Version: version.MustParseBinary("2.9.0-ubuntu-amd64"),
			URL:     "http://example.com/tools",
		},
	}, nil
}

type mockDistributionGroupFinder struct{}

func (m *mockDistributionGroupFinder) DistributionGroupByMachineId(ctx context.Context, tags ...names.MachineTag) ([]apiprovisioner.DistributionGroupResult, error) {
	results := make([]apiprovisioner.DistributionGroupResult, len(tags))
	for i := range tags {
		results[i] = apiprovisioner.DistributionGroupResult{
			Result: []instance.Id{},
		}
	}
	return results, nil
}

type testLogger struct {
	c *gc.C
}

func (l *testLogger) Criticalf(format string, args ...interface{}) {
	l.c.Logf("CRITICAL: "+format, args...)
}
func (l *testLogger) Errorf(format string, args ...interface{}) { l.c.Logf("ERROR: "+format, args...) }
func (l *testLogger) Warningf(format string, args ...interface{}) {
	l.c.Logf("WARNING: "+format, args...)
}
func (l *testLogger) Infof(format string, args ...interface{})  { l.c.Logf("INFO: "+format, args...) }
func (l *testLogger) Debugf(format string, args ...interface{}) { l.c.Logf("DEBUG: "+format, args...) }
func (l *testLogger) Tracef(format string, args ...interface{}) { l.c.Logf("TRACE: "+format, args...) }
func (l *testLogger) IsLevelEnabled(level logger.Level) bool    { return true }
func (l *testLogger) Child(name string) logger.Logger           { return l }
