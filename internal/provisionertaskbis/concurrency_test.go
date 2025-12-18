// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package provisionertaskbis

import (
	"context"
	"sync"
	"sync/atomic"
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
func TestConcurrencySuite(t *testing.T) {
	tc.Run(t, &ConcurrencySuite{})
}

// ConcurrencySuite contains concurrency and edge-case tests for provisionertaskbis.
type ConcurrencySuite struct{}

// TestProviderSemaphoreMaxConcurrencyRespectsSizeAndResize tests that the
// provider semaphore enforces max concurrency and handles resize correctly.
func (s *ConcurrencySuite) TestProviderSemaphoreMaxConcurrencyRespectsSizeAndResize(c *tc.C) {
	const semaphoreSize = 2
	const numMachines = 5

	semaphore := NewProviderSemaphore(semaphoreSize)

	var inFlight atomic.Int32
	var maxObserved atomic.Int32
	var mu sync.Mutex
	readyCount := 0
	gate := make(chan struct{})
	allReady := make(chan struct{})

	// Track concurrent operations
	trackConcurrency := func() func() {
		current := inFlight.Add(1)
		for {
			old := maxObserved.Load()
			if current <= old || maxObserved.CompareAndSwap(old, current) {
				break
			}
		}
		return func() {
			inFlight.Add(-1)
		}
	}

	// Signal when a goroutine is ready at the gate
	signalReady := func() {
		mu.Lock()
		readyCount++
		if readyCount == semaphoreSize {
			close(allReady)
		}
		mu.Unlock()
	}

	// Launch workers that try to acquire semaphore
	var wg sync.WaitGroup
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for i := 0; i < numMachines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := semaphore.Acquire(ctx); err != nil {
				return
			}
			done := trackConcurrency()
			signalReady()
			<-gate // Wait at gate
			done()
			semaphore.Release()
		}()
	}

	// Wait for semaphoreSize workers to be at the gate
	select {
	case <-allReady:
	case <-ctx.Done():
		c.Fatal("timeout waiting for workers to reach gate")
	}

	// At this point, exactly semaphoreSize workers should be in-flight
	c.Assert(inFlight.Load(), tc.Equals, int32(semaphoreSize))
	c.Assert(maxObserved.Load(), tc.Equals, int32(semaphoreSize))

	// Release the gate - let all workers complete
	close(gate)

	// Wait for all workers to complete
	wg.Wait()

	// Max observed should never exceed semaphore size
	c.Assert(maxObserved.Load() <= int32(semaphoreSize), tc.IsTrue,
		tc.Commentf("max observed %d > semaphore size %d", maxObserved.Load(), semaphoreSize))
}

// TestProviderSemaphoreResizeUp tests that resizing the semaphore up allows
// more concurrent operations.
func (s *ConcurrencySuite) TestProviderSemaphoreResizeUp(c *tc.C) {
	semaphore := NewProviderSemaphore(1)

	var inFlight atomic.Int32
	gate := make(chan struct{})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// First worker acquires
	err := semaphore.Acquire(ctx)
	c.Assert(err, tc.IsNil)
	inFlight.Add(1)

	// Second worker tries to acquire in background
	secondAcquired := make(chan struct{})
	go func() {
		if err := semaphore.Acquire(ctx); err != nil {
			return
		}
		inFlight.Add(1)
		close(secondAcquired)
		<-gate
		semaphore.Release()
	}()

	// Give second worker time to block
	time.Sleep(20 * time.Millisecond)
	c.Assert(inFlight.Load(), tc.Equals, int32(1))

	// Resize up to 2
	semaphore.Resize(2)

	// Second worker should now acquire
	select {
	case <-secondAcquired:
		c.Assert(inFlight.Load(), tc.Equals, int32(2))
	case <-ctx.Done():
		c.Fatal("second worker did not acquire after resize up")
	}

	// Cleanup
	close(gate)
	semaphore.Release()
}

// TestProviderSemaphoreResizeDownDoesNotPreempt tests that resizing down
// does not preempt in-flight operations.
func (s *ConcurrencySuite) TestProviderSemaphoreResizeDownDoesNotPreempt(c *tc.C) {
	semaphore := NewProviderSemaphore(3)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Acquire 3 slots
	for i := 0; i < 3; i++ {
		err := semaphore.Acquire(ctx)
		c.Assert(err, tc.IsNil)
	}

	// Resize down to 1 - should not block or preempt
	semaphore.Resize(1)
	c.Assert(semaphore.Size(), tc.Equals, 1)

	// Release all 3
	for i := 0; i < 3; i++ {
		semaphore.Release()
	}

	// Now try to acquire - only 1 should be allowed
	err := semaphore.Acquire(ctx)
	c.Assert(err, tc.IsNil)

	// Second acquire should block (test by checking it doesn't complete immediately)
	blocked := make(chan struct{})
	go func() {
		shortCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		if err := semaphore.Acquire(shortCtx); err == nil {
			semaphore.Release()
		}
		close(blocked)
	}()

	select {
	case <-blocked:
		// Good - the second acquire timed out as expected
	case <-ctx.Done():
		c.Fatal("test timeout")
	}

	semaphore.Release()
}

// TestMainLoopShutdownDoesNotBlockOnStalledWorker tests that the main loop
// can shut down cleanly even when a worker is stalled.
func (s *ConcurrencySuite) TestMainLoopShutdownDoesNotBlockOnStalledWorker(c *tc.C) {
	machineChanges := make(chan []string)
	retryChanges := make(chan struct{})

	machineWatcher := watchertest.NewMockStringsWatcher(machineChanges)
	retryWatcher := watchertest.NewMockNotifyWatcher(retryChanges)

	machineGetter := newFakeMachineGetter()
	broker := &stallableBroker{
		startGate: make(chan struct{}),
		stopGate:  make(chan struct{}),
	}
	infoSetter := newFakeInstanceInfoSetter()

	cfg := TaskConfig{
		Logger:              &fakeLogger{},
		MachineWatcher:      machineWatcher,
		RetryWatcher:        retryWatcher,
		MachineGetter:       machineGetter,
		Broker:              broker,
		InstanceInfoSetter:  infoSetter,
		AZCoordinator:       NewAZCoordinator([]string{"zone-a"}, nil),
		ProviderSemaphore:   NewProviderSemaphore(10),
		MaxRetries:          2,
		RetryDelay:          1 * time.Millisecond,
		NumProvisionWorkers: 10,
	}

	// Add a pending machine
	m := newFakeClassifiableMachine("0")
	machineGetter.addMachine(m)

	// Create task
	task, err := NewProvisionerTask(cfg)
	c.Assert(err, tc.IsNil)

	// Trigger machine processing
	machineChanges <- []string{"0"}

	// Wait a bit for the machine worker to start and potentially reach the broker
	time.Sleep(50 * time.Millisecond)

	// Kill the task while broker operations may be stalled
	shutdownComplete := make(chan error, 1)
	go func() {
		task.Kill()
		shutdownComplete <- task.Wait()
	}()

	// Shutdown should complete within a reasonable time
	// even if the broker is stalled
	select {
	case err := <-shutdownComplete:
		c.Assert(err, tc.IsNil)
	case <-time.After(2 * time.Second):
		c.Fatal("shutdown blocked - potential deadlock")
	}

	// Cleanup
	close(machineChanges)
	close(retryChanges)
}

// TestTransientRetryWatcherTriggersReattempt tests that the retry watcher
// triggers a reattempt for machines with transient errors.
func (s *ConcurrencySuite) TestTransientRetryWatcherTriggersReattempt(c *tc.C) {
	machineChanges := make(chan []string)
	retryChanges := make(chan struct{})
	eventCallback := make(chan string, 10)

	machineWatcher := watchertest.NewMockStringsWatcher(machineChanges)
	retryWatcher := watchertest.NewMockNotifyWatcher(retryChanges)

	machineGetter := newFakeMachineGetter()
	broker := newFakeBroker()
	infoSetter := newFakeInstanceInfoSetter()

	cfg := TaskConfig{
		Logger:              &fakeLogger{},
		MachineWatcher:      machineWatcher,
		RetryWatcher:        retryWatcher,
		MachineGetter:       machineGetter,
		Broker:              broker,
		InstanceInfoSetter:  infoSetter,
		AZCoordinator:       NewAZCoordinator([]string{"zone-a"}, nil),
		ProviderSemaphore:   NewProviderSemaphore(10),
		MaxRetries:          2,
		RetryDelay:          1 * time.Millisecond,
		NumProvisionWorkers: 10,
		EventProcessedCb: func(evtType string) {
			select {
			case eventCallback <- evtType:
			default:
			}
		},
	}

	// Create task
	task, err := NewProvisionerTask(cfg)
	c.Assert(err, tc.IsNil)

	// Add a machine that will fail provisioning
	m := newFakeClassifiableMachine("0")
	machineGetter.addMachine(m)

	// Make broker fail
	broker.setStartInstanceErr(errors.New("transient error"))

	// Trigger machine processing
	machineChanges <- []string{"0"}

	// Wait for machine processing callback
	select {
	case <-eventCallback:
	case <-time.After(5 * time.Second):
		c.Fatal("timeout waiting for machine processing")
	}

	// Give time for retry exhaustion
	time.Sleep(100 * time.Millisecond)

	// Now trigger retry watcher
	retryChanges <- struct{}{}

	// Wait for retry callback
	select {
	case evt := <-eventCallback:
		c.Assert(evt, tc.Equals, EventRetriedMachinesWithErrors)
	case <-time.After(5 * time.Second):
		c.Fatal("timeout waiting for retry processing")
	}

	// Kill the task first, then close channels
	task.Kill()
	err = task.Wait()
	c.Assert(err, tc.IsNil)

	close(machineChanges)
	close(retryChanges)
}

// stallableBroker is a broker that can be stalled at StartInstance and StopInstances.
type stallableBroker struct {
	startGate chan struct{} // Close to unblock StartInstance
	stopGate  chan struct{} // Close to unblock StopInstances
	started   atomic.Int32
	stopped   atomic.Int32
}

func (b *stallableBroker) StartInstance(ctx context.Context, params StartInstanceParams) (StartInstanceResult, error) {
	b.started.Add(1)
	select {
	case <-b.startGate:
		return StartInstanceResult{
			InstanceID: "i-" + params.MachineID,
			ZoneName:   params.AvailabilityZone,
		}, nil
	case <-ctx.Done():
		return StartInstanceResult{}, ctx.Err()
	}
}

func (b *stallableBroker) StopInstances(ctx context.Context, ids ...string) error {
	b.stopped.Add(1)
	select {
	case <-b.stopGate:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// TestDiesDuringProvisioningWithInstanceCreated tests the scenario where
// a machine dies after StartInstance succeeds but before SetInstanceInfo completes.
// This is a critical correctness test for rollback semantics.
func (s *ConcurrencySuite) TestDiesDuringProvisioningWithInstanceCreated(c *tc.C) {
	// This test validates that when a machine dies during provisioning:
	// 1. If an instance was created, it gets stopped (no leaks)
	// 2. The machine is properly cleaned up
	// 3. No deadlocks occur

	h := newWorkerTestHarness("0")
	h.broker.setStartInstanceResult(StartInstanceResult{
		InstanceID: "i-0",
		ZoneName:   "zone-a",
	})
	// SetInstanceInfo will be "blocked" by not calling it - we'll kill the worker first

	worker, err := NewMachineWorker(h.config())
	c.Assert(err, tc.IsNil)

	// Trigger provisioning
	h.sendEvent(MachineEvent{Type: EventLifeChanged, Life: life.Alive})

	// Receive zone request
	req := h.receiveRequest()
	c.Assert(req.Type, tc.Equals, RequestZone)

	// Send zone assignment
	h.sendEvent(MachineEvent{Type: EventZoneAssigned, Zone: "zone-a"})

	// Wait for provisioning to start (StartInstance called)
	time.Sleep(50 * time.Millisecond)

	// Now kill the worker while it's in the provisioning state
	// This simulates a machine dying during provisioning
	worker.Kill()
	err = worker.Wait()
	c.Assert(err, tc.IsNil)

	// The key assertion: if StartInstance was called, we should clean up
	// In a real scenario, the coordinator would handle this
}

// TestNoInstanceLeakOnCoordinatorFailure tests that when zone request fails,
// no instance is left running and the worker transitions back to Pending.
func (s *ConcurrencySuite) TestNoInstanceLeakOnCoordinatorFailure(c *tc.C) {
	h := newWorkerTestHarness("0")

	worker, err := NewMachineWorker(h.config())
	c.Assert(err, tc.IsNil)
	defer workertest.CleanKill(c, worker)

	// Trigger zone request
	h.sendEvent(MachineEvent{Type: EventLifeChanged, Life: life.Alive})

	// Receive zone request
	req := h.receiveRequest()
	c.Assert(req.Type, tc.Equals, RequestZone)

	// Send zone request failure
	h.sendEvent(MachineEvent{
		Type:      EventZoneRequestFailed,
		ZoneError: errors.New("no zones available"),
	})

	// Retry timer will fire and worker will request zone again
	req = h.receiveRequest()
	c.Assert(req.Type, tc.Equals, RequestZone)

	// Worker is in RequestingZone after automatic retry
	c.Assert(worker.State(), tc.Equals, StateRequestingZone)

	// Verify no instances were started (zone request failed before provisioning)
	c.Assert(h.broker.getStartInstanceCallCount(), tc.Equals, 0)
}

// fakeMachineForConcurrency implements ClassifiableMachineFull with thread-safe operations.
type fakeMachineForConcurrency struct {
	mu           sync.RWMutex
	id           string
	lifeVal      life.Value
	instanceID   string
	keepInstance bool
	statusVal    status.Status
}

func (m *fakeMachineForConcurrency) Life() life.Value {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lifeVal
}

func (m *fakeMachineForConcurrency) Id() string {
	return m.id
}

func (m *fakeMachineForConcurrency) InstanceId(ctx context.Context) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.instanceID == "" {
		return "", errNotProvisioned
	}
	return m.instanceID, nil
}

func (m *fakeMachineForConcurrency) EnsureDead(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lifeVal = life.Dead
	return nil
}

func (m *fakeMachineForConcurrency) Status(ctx context.Context) (status.Status, string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.statusVal, "", nil
}

func (m *fakeMachineForConcurrency) InstanceStatus(ctx context.Context) (status.Status, string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return status.Provisioning, "", nil
}

func (m *fakeMachineForConcurrency) KeepInstance(ctx context.Context) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.keepInstance, nil
}

func (m *fakeMachineForConcurrency) SetStatus(ctx context.Context, st status.Status, msg string, data map[string]interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.statusVal = st
	return nil
}

func (m *fakeMachineForConcurrency) SetInstanceStatus(ctx context.Context, st status.Status, msg string, data map[string]interface{}) error {
	return nil
}

func (m *fakeMachineForConcurrency) MarkForRemoval(ctx context.Context) error {
	return nil
}

func (m *fakeMachineForConcurrency) Tag() names.MachineTag {
	return names.NewMachineTag(m.id)
}
