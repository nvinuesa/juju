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

	"github.com/juju/juju/core/life"
	"github.com/juju/juju/core/status"
)

// Test suite runner
func TestCompatibilitySuite(t *testing.T) {
	tc.Run(t, &CompatibilitySuite{})
}

// CompatibilitySuite contains behavioral compatibility tests that validate
// the FSM implementation produces the same observable outcomes as expected
// from the legacy provisioner task.
//
// Key point: We assert OUTCOMES and INVARIANTS, not call sequences.
// The FSM architecture is different, but final state must be equivalent.
type CompatibilitySuite struct{}

// FakeState is a stateful simulation of the world for compatibility testing.
// Both old and new implementations mutate this through fakes.
type FakeState struct {
	mu sync.RWMutex

	// Machines keyed by machine ID
	Machines map[string]*FakeMachineState

	// Instances keyed by instance ID
	Instances map[string]*FakeInstanceState

	// StateChanged is signaled whenever any mutation occurs
	StateChanged chan StateChangeEvent
}

// StateChangeEvent represents a state mutation for deterministic waiting.
type StateChangeEvent struct {
	Reason    string
	MachineID string
}

// FakeMachineState represents the state of a machine in the simulation.
type FakeMachineState struct {
	ID                   string
	Life                 life.Value
	Status               status.Status
	StatusInfo           string
	InstanceStatus       status.Status
	InstanceStatusInfo   string
	InstanceID           string
	Zone                 string
	KeepInstance         bool
	Removed              bool
	EnsureDeadCalled     bool
	MarkForRemovalCalled bool
}

// FakeInstanceState represents the state of an instance in the simulation.
type FakeInstanceState struct {
	ID      string
	Zone    string
	Running bool
}

// NewFakeState creates a new empty FakeState.
func NewFakeState() *FakeState {
	return &FakeState{
		Machines:     make(map[string]*FakeMachineState),
		Instances:    make(map[string]*FakeInstanceState),
		StateChanged: make(chan StateChangeEvent, 100),
	}
}

// AddMachine adds a machine to the state.
func (s *FakeState) AddMachine(id string, lifeVal life.Value, statusVal status.Status) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Machines[id] = &FakeMachineState{
		ID:             id,
		Life:           lifeVal,
		Status:         statusVal,
		InstanceStatus: status.Provisioning,
	}
	s.signalChange("machine-added", id)
}

// AddMachineWithInstance adds a provisioned machine with an instance.
func (s *FakeState) AddMachineWithInstance(id string, lifeVal life.Value, instanceID string, zone string, keepInstance bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Machines[id] = &FakeMachineState{
		ID:             id,
		Life:           lifeVal,
		Status:         status.Started,
		InstanceStatus: status.Running,
		InstanceID:     instanceID,
		Zone:           zone,
		KeepInstance:   keepInstance,
	}
	s.Instances[instanceID] = &FakeInstanceState{
		ID:      instanceID,
		Zone:    zone,
		Running: true,
	}
	s.signalChange("machine-with-instance-added", id)
}

// GetMachine returns a copy of machine state.
func (s *FakeState) GetMachine(id string) *FakeMachineState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if m, ok := s.Machines[id]; ok {
		copy := *m
		return &copy
	}
	return nil
}

// GetInstance returns a copy of instance state.
func (s *FakeState) GetInstance(id string) *FakeInstanceState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if i, ok := s.Instances[id]; ok {
		copy := *i
		return &copy
	}
	return nil
}

// RunningInstanceCount returns the count of running instances.
func (s *FakeState) RunningInstanceCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	count := 0
	for _, inst := range s.Instances {
		if inst.Running {
			count++
		}
	}
	return count
}

// signalChange sends an event on StateChanged (must hold lock).
func (s *FakeState) signalChange(reason, machineID string) {
	select {
	case s.StateChanged <- StateChangeEvent{Reason: reason, MachineID: machineID}:
	default:
	}
}

// WaitForCondition waits for a condition to be true, checking after each state change.
func (s *FakeState) WaitForCondition(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return true
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return false
		}
		select {
		case <-s.StateChanged:
			// Check condition again
		case <-time.After(remaining):
			return cond()
		}
	}
}

// statefulFakeMachine implements ClassifiableMachineFull backed by FakeState.
type statefulFakeMachine struct {
	id    string
	state *FakeState
}

func (m *statefulFakeMachine) Life() life.Value {
	machine := m.state.GetMachine(m.id)
	if machine == nil {
		return life.Dead
	}
	return machine.Life
}

func (m *statefulFakeMachine) ID() string {
	return m.id
}

func (m *statefulFakeMachine) InstanceID() string {
	machine := m.state.GetMachine(m.id)
	if machine == nil {
		return ""
	}
	return machine.InstanceID
}

func (m *statefulFakeMachine) EnsureDead(ctx context.Context) error {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	if machine, ok := m.state.Machines[m.id]; ok {
		machine.Life = life.Dead
		machine.EnsureDeadCalled = true
		m.state.signalChange("ensure-dead", m.id)
	}
	return nil
}

func (m *statefulFakeMachine) KeepInstance() bool {
	machine := m.state.GetMachine(m.id)
	if machine == nil {
		return false
	}
	return machine.KeepInstance
}

func (m *statefulFakeMachine) SetStatus(ctx context.Context, st status.Status, msg string) error {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	if machine, ok := m.state.Machines[m.id]; ok {
		machine.Status = st
		machine.StatusInfo = msg
		m.state.signalChange("status-set", m.id)
	}
	return nil
}

func (m *statefulFakeMachine) SetInstanceStatus(ctx context.Context, st status.Status, msg string) error {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	if machine, ok := m.state.Machines[m.id]; ok {
		machine.InstanceStatus = st
		machine.InstanceStatusInfo = msg
		m.state.signalChange("instance-status-set", m.id)
	}
	return nil
}

func (m *statefulFakeMachine) MarkForRemoval(ctx context.Context) error {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	if machine, ok := m.state.Machines[m.id]; ok {
		machine.Removed = true
		machine.MarkForRemovalCalled = true
		m.state.signalChange("marked-for-removal", m.id)
	}
	return nil
}

func (m *statefulFakeMachine) Tag() names.MachineTag {
	return names.NewMachineTag(m.id)
}

// statefulFakeBroker implements BrokerFacade backed by FakeState.
type statefulFakeBroker struct {
	state           *FakeState
	startErr        error
	stopErr         error
	instanceCounter int
	mu              sync.Mutex
}

func (b *statefulFakeBroker) StartInstance(ctx context.Context, params StartInstanceParams) (StartInstanceResult, error) {
	if b.startErr != nil {
		return StartInstanceResult{}, b.startErr
	}

	b.mu.Lock()
	b.instanceCounter++
	instanceID := "i-" + params.MachineID
	b.mu.Unlock()

	zone := params.AvailabilityZone
	if zone == "" {
		zone = "zone-a"
	}

	b.state.mu.Lock()
	b.state.Instances[instanceID] = &FakeInstanceState{
		ID:      instanceID,
		Zone:    zone,
		Running: true,
	}
	b.state.signalChange("instance-started", params.MachineID)
	b.state.mu.Unlock()

	return StartInstanceResult{
		InstanceID: instanceID,
		ZoneName:   zone,
	}, nil
}

func (b *statefulFakeBroker) StopInstances(ctx context.Context, instanceIDs ...string) error {
	if b.stopErr != nil {
		return b.stopErr
	}

	b.state.mu.Lock()
	defer b.state.mu.Unlock()

	for _, id := range instanceIDs {
		if inst, ok := b.state.Instances[id]; ok {
			inst.Running = false
			b.state.signalChange("instance-stopped", id)
		}
	}
	return nil
}

// statefulFakeInstanceInfoSetter implements InstanceInfoSetter backed by FakeState.
type statefulFakeInstanceInfoSetter struct {
	state  *FakeState
	setErr error
}

func (s *statefulFakeInstanceInfoSetter) SetInstanceInfo(ctx context.Context, machineID, instanceID, zoneName string) error {
	if s.setErr != nil {
		return s.setErr
	}

	s.state.mu.Lock()
	defer s.state.mu.Unlock()

	if machine, ok := s.state.Machines[machineID]; ok {
		machine.InstanceID = instanceID
		machine.Zone = zoneName
		machine.InstanceStatus = status.Running
		s.state.signalChange("instance-info-set", machineID)
	}
	return nil
}

// Compatibility Scenarios

// TestScenario1HappyPathProvisioning tests that a pending machine gets provisioned.
func (s *CompatibilitySuite) TestScenario1HappyPathProvisioning(c *tc.C) {
	fakeState := NewFakeState()
	fakeState.AddMachine("0", life.Alive, status.Pending)

	// Create FSM worker
	h := newCompatibilityHarness("0", fakeState)

	worker, err := NewMachineWorker(h.config())
	c.Assert(err, tc.IsNil)

	// Trigger provisioning
	h.sendLifeAlive()
	h.waitForZoneRequest(c)
	h.sendZoneAssigned("zone-a")

	// Wait for provisioning to complete
	ok := fakeState.WaitForCondition(5*time.Second, func() bool {
		m := fakeState.GetMachine("0")
		return m != nil && m.InstanceID != ""
	})
	c.Assert(ok, tc.IsTrue, tc.Commentf("provisioning did not complete"))

	worker.Kill()
	c.Assert(worker.Wait(), tc.IsNil)

	// Assert outcomes
	machine := fakeState.GetMachine("0")
	c.Assert(machine, tc.NotNil)
	c.Assert(machine.InstanceID, tc.Not(tc.Equals), "")
	c.Assert(machine.Removed, tc.IsFalse)
	c.Assert(machine.Status != status.Error, tc.IsTrue)

	instance := fakeState.GetInstance(machine.InstanceID)
	c.Assert(instance, tc.NotNil)
	c.Assert(instance.Running, tc.IsTrue)
}

// TestScenario2OrphanCleanupDeadMachineWithoutInstance tests dead machine cleanup.
func (s *CompatibilitySuite) TestScenario2OrphanCleanupDeadMachineWithoutInstance(c *tc.C) {
	fakeState := NewFakeState()
	fakeState.AddMachine("0", life.Dead, status.Pending)

	h := newCompatibilityHarness("0", fakeState)

	worker, err := NewMachineWorker(h.config())
	c.Assert(err, tc.IsNil)

	// Trigger dead machine handling
	h.sendLifeDead()

	// Wait for removal
	ok := fakeState.WaitForCondition(5*time.Second, func() bool {
		m := fakeState.GetMachine("0")
		return m != nil && m.MarkForRemovalCalled
	})
	c.Assert(ok, tc.IsTrue, tc.Commentf("machine was not marked for removal"))

	err = worker.Wait()
	c.Assert(err, tc.IsNil)

	// Assert outcomes
	machine := fakeState.GetMachine("0")
	c.Assert(machine.MarkForRemovalCalled, tc.IsTrue)
	c.Assert(fakeState.RunningInstanceCount(), tc.Equals, 0)
}

// TestScenario3DeadMachineWithInstanceStopThenRemove tests stopping instance on death.
func (s *CompatibilitySuite) TestScenario3DeadMachineWithInstanceStopThenRemove(c *tc.C) {
	fakeState := NewFakeState()
	fakeState.AddMachineWithInstance("0", life.Alive, "i-0", "zone-a", false)

	h := newCompatibilityHarness("0", fakeState)

	worker, err := NewMachineWorker(h.config())
	c.Assert(err, tc.IsNil)

	// First send alive to get to Running state
	h.sendLifeAlive()
	time.Sleep(20 * time.Millisecond)

	// Now send dead
	h.sendLifeDead()

	// Wait for instance to be stopped and machine removed
	ok := fakeState.WaitForCondition(5*time.Second, func() bool {
		m := fakeState.GetMachine("0")
		i := fakeState.GetInstance("i-0")
		return m != nil && m.MarkForRemovalCalled && i != nil && !i.Running
	})
	c.Assert(ok, tc.IsTrue, tc.Commentf("machine not removed or instance not stopped"))

	err = worker.Wait()
	c.Assert(err, tc.IsNil)

	// Assert outcomes
	machine := fakeState.GetMachine("0")
	c.Assert(machine.MarkForRemovalCalled, tc.IsTrue)

	instance := fakeState.GetInstance("i-0")
	c.Assert(instance.Running, tc.IsFalse)
}

// TestScenario4KeepInstanceSemantics tests that keep-instance prevents stopping.
func (s *CompatibilitySuite) TestScenario4KeepInstanceSemantics(c *tc.C) {
	fakeState := NewFakeState()
	fakeState.AddMachineWithInstance("0", life.Alive, "i-0", "zone-a", true) // keep-instance=true

	h := newCompatibilityHarness("0", fakeState)

	worker, err := NewMachineWorker(h.config())
	c.Assert(err, tc.IsNil)

	// First send alive to get to Running state
	h.sendLifeAlive()
	time.Sleep(20 * time.Millisecond)

	// Now send dead
	h.sendLifeDead()

	// Wait for machine to be removed
	ok := fakeState.WaitForCondition(5*time.Second, func() bool {
		m := fakeState.GetMachine("0")
		return m != nil && m.MarkForRemovalCalled
	})
	c.Assert(ok, tc.IsTrue, tc.Commentf("machine was not removed"))

	err = worker.Wait()
	c.Assert(err, tc.IsNil)

	// Assert outcomes: machine removed, but instance still running
	machine := fakeState.GetMachine("0")
	c.Assert(machine.MarkForRemovalCalled, tc.IsTrue)

	instance := fakeState.GetInstance("i-0")
	c.Assert(instance, tc.NotNil)
	c.Assert(instance.Running, tc.IsTrue, tc.Commentf("instance should remain running with keep-instance"))
}

// TestScenario6RetryExhaustionProvisioningError tests that retry exhaustion leads to error.
func (s *CompatibilitySuite) TestScenario6RetryExhaustionProvisioningError(c *tc.C) {
	fakeState := NewFakeState()
	fakeState.AddMachine("0", life.Alive, status.Pending)

	h := newCompatibilityHarness("0", fakeState)
	h.broker.startErr = errors.New("provider unavailable")
	h.maxRetries = 2

	worker, err := NewMachineWorker(h.config())
	c.Assert(err, tc.IsNil)

	// Trigger provisioning
	h.sendLifeAlive()

	// Wait for zone requests and failures - retry timer fires automatically
	for i := 0; i < 3; i++ { // Initial + 2 retries
		h.waitForZoneRequest(c)
		h.sendZoneAssigned("zone-a")
		time.Sleep(20 * time.Millisecond)
		// Retry timer will automatically trigger the next attempt (if retries remain)
	}

	// Wait for provisioning error status (FSM sets InstanceStatus, not Status)
	ok := fakeState.WaitForCondition(5*time.Second, func() bool {
		m := fakeState.GetMachine("0")
		return m != nil && m.InstanceStatus == status.ProvisioningError
	})
	c.Assert(ok, tc.IsTrue, tc.Commentf("machine did not end in ProvisioningError status"))

	worker.Kill()
	c.Assert(worker.Wait(), tc.IsNil)

	// Assert outcomes
	machine := fakeState.GetMachine("0")
	c.Assert(machine.InstanceStatus, tc.Equals, status.ProvisioningError)
	c.Assert(machine.InstanceID, tc.Equals, "") // No instance was created
	c.Assert(fakeState.RunningInstanceCount(), tc.Equals, 0)
}

// compatibilityHarness provides test setup for compatibility scenarios.
type compatibilityHarness struct {
	machineID   string
	state       *FakeState
	events      chan MachineEvent
	requests    chan WorkerRequest
	broker      *statefulFakeBroker
	infoSetter  *statefulFakeInstanceInfoSetter
	maxRetries  int
	retryDelay  time.Duration
}

func newCompatibilityHarness(machineID string, state *FakeState) *compatibilityHarness {
	return &compatibilityHarness{
		machineID:   machineID,
		state:       state,
		events:      make(chan MachineEvent, 10),
		requests:    make(chan WorkerRequest, 10),
		broker:      &statefulFakeBroker{state: state},
		infoSetter:  &statefulFakeInstanceInfoSetter{state: state},
		maxRetries:  2,
		retryDelay:  1 * time.Millisecond, // Very short for tests
	}
}

func (h *compatibilityHarness) config() MachineWorkerConfig {
	return MachineWorkerConfig{
		MachineID:          h.machineID,
		Machine:            &statefulFakeMachine{id: h.machineID, state: h.state},
		Broker:             h.broker,
		InstanceInfoSetter: h.infoSetter,
		Semaphore:          NewProviderSemaphore(10),
		Logger:             &fakeLogger{},
		EventsChan:         h.events,
		RequestChan:        h.requests,
		MaxRetries:         h.maxRetries,
		RetryDelay:         h.retryDelay,
	}
}

func (h *compatibilityHarness) sendLifeAlive() {
	h.events <- MachineEvent{Type: EventLifeChanged, Life: life.Alive}
}

func (h *compatibilityHarness) sendLifeDead() {
	h.events <- MachineEvent{Type: EventLifeChanged, Life: life.Dead}
}

func (h *compatibilityHarness) sendZoneAssigned(zone string) {
	h.events <- MachineEvent{Type: EventZoneAssigned, Zone: zone}
}

func (h *compatibilityHarness) waitForZoneRequest(c *tc.C) {
	deadline := time.After(5 * time.Second)
	for {
		select {
		case req := <-h.requests:
			if req.Type == RequestZone {
				return
			}
			// Skip non-zone requests (e.g., RequestProvisionComplete from failures)
		case <-deadline:
			c.Fatal("timeout waiting for zone request")
		}
	}
}
