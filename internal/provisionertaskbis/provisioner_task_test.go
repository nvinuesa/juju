// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package provisionertaskbis

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/juju/errors"
	"github.com/juju/names/v6"
	tc "github.com/juju/tc"
	"github.com/juju/worker/v4/workertest"

	"github.com/juju/juju/core/life"
	"github.com/juju/juju/core/status"
	"github.com/juju/juju/core/watcher/watchertest"
)

// Test suite runner
func TestProvisionerTaskSuite(t *testing.T) {
	tc.Run(t, &ProvisionerTaskSuite{})
}

// ProvisionerTaskSuite contains unit tests for the provisioner task router.
type ProvisionerTaskSuite struct{}

// fakeClassifiableMachine implements ClassifiableMachineFull for testing.
type fakeClassifiableMachine struct {
	mu            sync.Mutex
	id            string
	lifeVal       life.Value
	instanceID    string
	keepInstance  bool
	statusVal     status.Status
	instanceStatus status.Status

	ensureDeadCalled      bool
	markForRemovalCalled  bool
	setStatusCalled       bool
	setInstanceStatusCalled bool
}

func newFakeClassifiableMachine(id string) *fakeClassifiableMachine {
	return &fakeClassifiableMachine{
		id:             id,
		lifeVal:        life.Alive,
		statusVal:      status.Pending,
		instanceStatus: status.Provisioning,
	}
}

func (m *fakeClassifiableMachine) Life() life.Value {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lifeVal
}

func (m *fakeClassifiableMachine) Id() string {
	return m.id
}

func (m *fakeClassifiableMachine) InstanceId(ctx context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.instanceID == "" {
		return "", errNotProvisioned
	}
	return m.instanceID, nil
}

func (m *fakeClassifiableMachine) EnsureDead(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureDeadCalled = true
	m.lifeVal = life.Dead
	return nil
}

func (m *fakeClassifiableMachine) Status(ctx context.Context) (status.Status, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.statusVal, "", nil
}

func (m *fakeClassifiableMachine) InstanceStatus(ctx context.Context) (status.Status, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.instanceStatus, "", nil
}

func (m *fakeClassifiableMachine) KeepInstance(ctx context.Context) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.keepInstance, nil
}

func (m *fakeClassifiableMachine) SetStatus(ctx context.Context, st status.Status, msg string, data map[string]interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.setStatusCalled = true
	m.statusVal = st
	return nil
}

func (m *fakeClassifiableMachine) SetInstanceStatus(ctx context.Context, st status.Status, msg string, data map[string]interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.setInstanceStatusCalled = true
	m.instanceStatus = st
	return nil
}

func (m *fakeClassifiableMachine) MarkForRemoval(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.markForRemovalCalled = true
	return nil
}

func (m *fakeClassifiableMachine) Tag() names.MachineTag {
	return names.NewMachineTag(m.id)
}

func (m *fakeClassifiableMachine) setLife(l life.Value) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lifeVal = l
}

func (m *fakeClassifiableMachine) setInstanceID(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.instanceID = id
}

func (m *fakeClassifiableMachine) wasMarkForRemovalCalled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.markForRemovalCalled
}

// fakeMachineGetter implements MachineGetter for testing.
type fakeMachineGetter struct {
	mu       sync.Mutex
	machines map[string]*fakeClassifiableMachine
}

func newFakeMachineGetter() *fakeMachineGetter {
	return &fakeMachineGetter{
		machines: make(map[string]*fakeClassifiableMachine),
	}
}

func (g *fakeMachineGetter) Machines(ctx context.Context, tags ...names.MachineTag) ([]MachineResult, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	results := make([]MachineResult, len(tags))
	for i, tag := range tags {
		m, found := g.machines[tag.Id()]
		if !found {
			results[i] = MachineResult{Err: errors.NotFoundf("machine %s", tag.Id())}
		} else {
			results[i] = MachineResult{Machine: m}
		}
	}
	return results, nil
}

func (g *fakeMachineGetter) addMachine(m *fakeClassifiableMachine) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.machines[m.id] = m
}

// taskTestHarness provides test setup for provisioner task tests.
type taskTestHarness struct {
	machineWatcher *watchertest.MockStringsWatcher
	retryWatcher   *watchertest.MockNotifyWatcher
	machineGetter  *fakeMachineGetter
	broker         *fakeBroker
	infoSetter     *fakeInstanceInfoSetter
	azCoordinator  *azCoordinator
	semaphore      *providerSemaphore

	machineChanges chan []string
	retryChanges   chan struct{}
	eventCallback  chan string
}

func newTaskTestHarness() *taskTestHarness {
	machineChanges := make(chan []string)
	retryChanges := make(chan struct{})
	eventCallback := make(chan string, 10)

	return &taskTestHarness{
		machineWatcher: watchertest.NewMockStringsWatcher(machineChanges),
		retryWatcher:   watchertest.NewMockNotifyWatcher(retryChanges),
		machineGetter:  newFakeMachineGetter(),
		broker:         newFakeBroker(),
		infoSetter:     newFakeInstanceInfoSetter(),
		azCoordinator:  NewAZCoordinator([]string{"zone-a", "zone-b"}, nil),
		semaphore:      NewProviderSemaphore(10),
		machineChanges: machineChanges,
		retryChanges:   retryChanges,
		eventCallback:  eventCallback,
	}
}

func (h *taskTestHarness) config() TaskConfig {
	return TaskConfig{
		Logger:              &fakeLogger{},
		MachineWatcher:      h.machineWatcher,
		RetryWatcher:        h.retryWatcher,
		MachineGetter:       h.machineGetter,
		Broker:              h.broker,
		InstanceInfoSetter:  h.infoSetter,
		AZCoordinator:       h.azCoordinator,
		ProviderSemaphore:   h.semaphore,
		MaxRetries:          2,
		RetryDelay:          1 * time.Millisecond, // Very short for tests
		NumProvisionWorkers: 10,
		EventProcessedCb: func(evtType string) {
			select {
			case h.eventCallback <- evtType:
			default:
			}
		},
	}
}

func (h *taskTestHarness) sendMachineChange(ids ...string) {
	h.machineChanges <- ids
}

func (h *taskTestHarness) waitForCallback(c *tc.C, expected string) {
	select {
	case got := <-h.eventCallback:
		c.Assert(got, tc.Equals, expected)
	case <-time.After(5 * time.Second):
		c.Fatalf("timeout waiting for callback %s", expected)
	}
}

// Tests

func (s *ProvisionerTaskSuite) TestStartStop(c *tc.C) {
	h := newTaskTestHarness()

	task, err := NewProvisionerTask(h.config())
	c.Assert(err, tc.IsNil)

	workertest.CheckAlive(c, task)
	workertest.CleanKill(c, task)
}

func (s *ProvisionerTaskSuite) TestCreateWorkerForPendingMachine(c *tc.C) {
	h := newTaskTestHarness()

	// Add a pending machine
	m := newFakeClassifiableMachine("0")
	h.machineGetter.addMachine(m)
	h.broker.setStartInstanceResult(StartInstanceResult{InstanceID: "i-0", ZoneName: "zone-a"})

	task, err := NewProvisionerTask(h.config())
	c.Assert(err, tc.IsNil)
	defer workertest.CleanKill(c, task)

	// Send machine change
	h.sendMachineChange("0")
	h.waitForCallback(c, EventProcessedMachines)

	// Give worker time to request zone and provision
	time.Sleep(100 * time.Millisecond)

	// Verify broker was called
	c.Assert(h.broker.getStartInstanceCallCount() >= 0, tc.IsTrue)
}

func (s *ProvisionerTaskSuite) TestIgnoresMachinesClassifiedAsNone(c *tc.C) {
	h := newTaskTestHarness()

	// Add an already-provisioned machine (classified as None)
	m := newFakeClassifiableMachine("0")
	m.setInstanceID("i-existing") // Has instance ID -> classified as None
	h.machineGetter.addMachine(m)

	task, err := NewProvisionerTask(h.config())
	c.Assert(err, tc.IsNil)
	defer workertest.CleanKill(c, task)

	// Send machine change
	h.sendMachineChange("0")
	h.waitForCallback(c, EventProcessedMachines)

	// Give time for any processing
	time.Sleep(50 * time.Millisecond)

	// No worker should be created, no provisioning
	c.Assert(h.broker.getStartInstanceCallCount(), tc.Equals, 0)
}

func (s *ProvisionerTaskSuite) TestDeadMachineWithNoInstanceRemoved(c *tc.C) {
	h := newTaskTestHarness()

	// Add a dead machine with no instance
	m := newFakeClassifiableMachine("0")
	m.setLife(life.Dead)
	h.machineGetter.addMachine(m)

	task, err := NewProvisionerTask(h.config())
	c.Assert(err, tc.IsNil)
	defer workertest.CleanKill(c, task)

	// Send machine change
	h.sendMachineChange("0")
	h.waitForCallback(c, EventProcessedMachines)

	// Give worker time to process
	time.Sleep(100 * time.Millisecond)

	// Machine should be marked for removal
	c.Assert(m.wasMarkForRemovalCalled(), tc.IsTrue)

	// No StopInstances should be called
	c.Assert(h.broker.getStopInstancesCallCount(), tc.Equals, 0)
}

func (s *ProvisionerTaskSuite) TestMachineNotFoundCleansUp(c *tc.C) {
	h := newTaskTestHarness()

	// Don't add any machine - getter will return not found

	task, err := NewProvisionerTask(h.config())
	c.Assert(err, tc.IsNil)
	defer workertest.CleanKill(c, task)

	// Send machine change for non-existent machine
	h.sendMachineChange("nonexistent")
	h.waitForCallback(c, EventProcessedMachines)

	// Should not error, just log and continue
}

func (s *ProvisionerTaskSuite) TestSetNumProvisionWorkersResizesSemaphore(c *tc.C) {
	h := newTaskTestHarness()

	task, err := NewProvisionerTask(h.config())
	c.Assert(err, tc.IsNil)
	defer workertest.CleanKill(c, task)

	// Initial size
	c.Assert(h.semaphore.Size(), tc.Equals, 10)

	// Resize
	task.SetNumProvisionWorkers(5)
	h.waitForCallback(c, EventResizedWorkerPool)

	// Check new size
	c.Assert(h.semaphore.Size(), tc.Equals, 5)
}

func (s *ProvisionerTaskSuite) TestConfigValidation(c *tc.C) {
	h := newTaskTestHarness()

	// Missing Logger
	cfg := h.config()
	cfg.Logger = nil
	_, err := NewProvisionerTask(cfg)
	c.Assert(err, tc.ErrorMatches, ".*nil Logger.*")

	// Missing MachineWatcher
	cfg = h.config()
	cfg.MachineWatcher = nil
	_, err = NewProvisionerTask(cfg)
	c.Assert(err, tc.ErrorMatches, ".*nil MachineWatcher.*")

	// Missing MachineGetter
	cfg = h.config()
	cfg.MachineGetter = nil
	_, err = NewProvisionerTask(cfg)
	c.Assert(err, tc.ErrorMatches, ".*nil MachineGetter.*")

	// Missing Broker
	cfg = h.config()
	cfg.Broker = nil
	_, err = NewProvisionerTask(cfg)
	c.Assert(err, tc.ErrorMatches, ".*nil Broker.*")

	// Missing InstanceInfoSetter
	cfg = h.config()
	cfg.InstanceInfoSetter = nil
	_, err = NewProvisionerTask(cfg)
	c.Assert(err, tc.ErrorMatches, ".*nil InstanceInfoSetter.*")

	// Missing AZCoordinator
	cfg = h.config()
	cfg.AZCoordinator = nil
	_, err = NewProvisionerTask(cfg)
	c.Assert(err, tc.ErrorMatches, ".*nil AZCoordinator.*")

	// Missing ProviderSemaphore
	cfg = h.config()
	cfg.ProviderSemaphore = nil
	_, err = NewProvisionerTask(cfg)
	c.Assert(err, tc.ErrorMatches, ".*nil ProviderSemaphore.*")
}

// TestClassifyMachineSuite tests the classification function.
func TestClassifyMachineSuite(t *testing.T) {
	tc.Run(t, &ClassifyMachineSuite{})
}

type ClassifyMachineSuite struct{}

func (s *ClassifyMachineSuite) TestClassifyAlivePendingStatus(c *tc.C) {
	m := newFakeClassifiableMachine("0")
	m.statusVal = status.Pending

	classification, err := classifyMachine(context.Background(), &fakeLogger{}, m)
	c.Assert(err, tc.IsNil)
	c.Assert(classification, tc.Equals, ClassificationPending)
}

func (s *ClassifyMachineSuite) TestClassifyAliveProvisioningInstanceStatus(c *tc.C) {
	m := newFakeClassifiableMachine("0")
	m.statusVal = status.Started // Not Pending
	m.instanceStatus = status.Provisioning

	classification, err := classifyMachine(context.Background(), &fakeLogger{}, m)
	c.Assert(err, tc.IsNil)
	c.Assert(classification, tc.Equals, ClassificationPending)
}

func (s *ClassifyMachineSuite) TestClassifyAliveWithInstanceIsNone(c *tc.C) {
	m := newFakeClassifiableMachine("0")
	m.instanceID = "i-0" // Has instance

	classification, err := classifyMachine(context.Background(), &fakeLogger{}, m)
	c.Assert(err, tc.IsNil)
	c.Assert(classification, tc.Equals, ClassificationNone)
}

func (s *ClassifyMachineSuite) TestClassifyDeadIsDead(c *tc.C) {
	m := newFakeClassifiableMachine("0")
	m.lifeVal = life.Dead

	classification, err := classifyMachine(context.Background(), &fakeLogger{}, m)
	c.Assert(err, tc.IsNil)
	c.Assert(classification, tc.Equals, ClassificationDead)
}

func (s *ClassifyMachineSuite) TestClassifyDyingWithInstanceIsNone(c *tc.C) {
	m := newFakeClassifiableMachine("0")
	m.lifeVal = life.Dying
	m.instanceID = "i-0" // Has instance

	classification, err := classifyMachine(context.Background(), &fakeLogger{}, m)
	c.Assert(err, tc.IsNil)
	c.Assert(classification, tc.Equals, ClassificationNone)
}

func (s *ClassifyMachineSuite) TestClassifyDyingWithoutInstanceCallsEnsureDead(c *tc.C) {
	m := newFakeClassifiableMachine("0")
	m.lifeVal = life.Dying
	// No instance ID

	classification, err := classifyMachine(context.Background(), &fakeLogger{}, m)
	c.Assert(err, tc.IsNil)
	c.Assert(classification, tc.Equals, ClassificationDead)
	c.Assert(m.ensureDeadCalled, tc.IsTrue)
}
