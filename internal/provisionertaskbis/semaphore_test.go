// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package provisionertaskbis

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tc "github.com/juju/tc"
)

// Test suite runner
func TestSemaphoreSuite(t *testing.T) {
	tc.Run(t, &SemaphoreSuite{})
}

// SemaphoreSuite contains unit tests for providerSemaphore.
type SemaphoreSuite struct{}

func (s *SemaphoreSuite) TestAcquireBlocksWhenFullAndReleaseUnblocks(c *tc.C) {
	sem := NewProviderSemaphore(1)
	ctx := context.Background()

	// Acquire first slot - should succeed immediately
	err := sem.Acquire(ctx)
	c.Assert(err, tc.IsNil)
	c.Assert(sem.heldCount(), tc.Equals, 1)

	// Start second acquire in goroutine - should block
	acquired := make(chan struct{})
	go func() {
		err := sem.Acquire(ctx)
		c.Check(err, tc.IsNil)
		close(acquired)
	}()

	// Give the goroutine time to start waiting
	time.Sleep(10 * time.Millisecond)

	// Verify waiter is blocked
	select {
	case <-acquired:
		c.Fatal("second Acquire should be blocked")
	default:
		// Expected - still waiting
	}

	// Release first slot
	sem.Release()

	// Second acquire should complete
	select {
	case <-acquired:
		// Expected
	case <-time.After(time.Second):
		c.Fatal("second Acquire should have completed after Release")
	}

	// Cleanup
	sem.Release()
}

func (s *SemaphoreSuite) TestResizeUpUnblocksWaiters(c *tc.C) {
	sem := NewProviderSemaphore(1)
	ctx := context.Background()

	// Fill the single slot
	err := sem.Acquire(ctx)
	c.Assert(err, tc.IsNil)

	// Start two waiters
	waiter1Done := make(chan struct{})
	waiter2Done := make(chan struct{})

	go func() {
		err := sem.Acquire(ctx)
		c.Check(err, tc.IsNil)
		close(waiter1Done)
	}()

	go func() {
		err := sem.Acquire(ctx)
		c.Check(err, tc.IsNil)
		close(waiter2Done)
	}()

	// Give goroutines time to start waiting
	time.Sleep(20 * time.Millisecond)

	// Both should be blocked
	select {
	case <-waiter1Done:
		c.Fatal("waiter1 should be blocked")
	case <-waiter2Done:
		c.Fatal("waiter2 should be blocked")
	default:
		// Expected
	}

	// Resize to 3 slots
	sem.Resize(3)

	// Both waiters should complete (we have 3 slots, 1 held + 2 waiting)
	select {
	case <-waiter1Done:
	case <-time.After(time.Second):
		c.Fatal("waiter1 should have completed after resize")
	}

	select {
	case <-waiter2Done:
	case <-time.After(time.Second):
		c.Fatal("waiter2 should have completed after resize")
	}

	// Cleanup
	sem.Release()
	sem.Release()
	sem.Release()
}

func (s *SemaphoreSuite) TestResizeDownDoesNotPreemptHolders(c *tc.C) {
	sem := NewProviderSemaphore(3)
	ctx := context.Background()

	// Acquire all 3 slots
	for i := 0; i < 3; i++ {
		err := sem.Acquire(ctx)
		c.Assert(err, tc.IsNil)
	}
	c.Assert(sem.heldCount(), tc.Equals, 3)

	// Resize down to 1
	sem.Resize(1)

	// Held count should still be 3 (not preempted)
	c.Assert(sem.heldCount(), tc.Equals, 3)
	c.Assert(sem.Size(), tc.Equals, 1)

	// Start a waiter
	waiterDone := make(chan struct{})
	go func() {
		err := sem.Acquire(ctx)
		c.Check(err, tc.IsNil)
		close(waiterDone)
	}()

	time.Sleep(10 * time.Millisecond)

	// Waiter should be blocked (held=3 >= size=1)
	select {
	case <-waiterDone:
		c.Fatal("waiter should be blocked")
	default:
	}

	// Release one - held=2, still >= size=1, waiter still blocked
	sem.Release()
	time.Sleep(10 * time.Millisecond)

	select {
	case <-waiterDone:
		c.Fatal("waiter should still be blocked with held=2 >= size=1")
	default:
	}

	// Release another - held=1, still >= size=1, waiter still blocked
	sem.Release()
	time.Sleep(10 * time.Millisecond)

	select {
	case <-waiterDone:
		c.Fatal("waiter should still be blocked with held=1 >= size=1")
	default:
	}

	// Release last one - held=0 < size=1, waiter can proceed
	sem.Release()

	select {
	case <-waiterDone:
		// Expected
	case <-time.After(time.Second):
		c.Fatal("waiter should have completed after all releases")
	}

	// Cleanup
	sem.Release()
}

func (s *SemaphoreSuite) TestAcquireRespectsContextCancellation(c *tc.C) {
	sem := NewProviderSemaphore(1)

	// Fill the slot
	err := sem.Acquire(context.Background())
	c.Assert(err, tc.IsNil)

	// Start an acquire with cancellable context
	ctx, cancel := context.WithCancel(context.Background())
	acquireDone := make(chan error, 1)

	go func() {
		err := sem.Acquire(ctx)
		acquireDone <- err
	}()

	// Give goroutine time to start waiting
	time.Sleep(10 * time.Millisecond)

	// Cancel the context
	cancel()

	// Acquire should return with context error
	select {
	case err := <-acquireDone:
		c.Assert(err, tc.Equals, context.Canceled)
	case <-time.After(time.Second):
		c.Fatal("Acquire should have returned after context cancellation")
	}

	// Verify cancellation didn't consume capacity
	c.Assert(sem.heldCount(), tc.Equals, 1)

	// Cleanup should work without double release
	sem.Release()
	c.Assert(sem.heldCount(), tc.Equals, 0)
}

func (s *SemaphoreSuite) TestConcurrentSafetySmoke(c *tc.C) {
	sem := NewProviderSemaphore(3)
	ctx := context.Background()

	var maxObserved int32
	var current int32
	var wg sync.WaitGroup

	// Start multiple goroutines doing acquire/release
	numWorkers := 10
	iterations := 20

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				err := sem.Acquire(ctx)
				if err != nil {
					continue
				}

				// Track current holders
				cur := atomic.AddInt32(&current, 1)
				for {
					max := atomic.LoadInt32(&maxObserved)
					if cur <= max {
						break
					}
					if atomic.CompareAndSwapInt32(&maxObserved, max, cur) {
						break
					}
				}

				// Do some work
				time.Sleep(time.Microsecond)

				atomic.AddInt32(&current, -1)
				sem.Release()
			}
		}()
	}

	wg.Wait()

	// Max observed should never exceed size
	c.Assert(atomic.LoadInt32(&maxObserved) <= int32(sem.Size()), tc.IsTrue,
		tc.Commentf("maxObserved=%d but size=%d", maxObserved, sem.Size()))
}

func (s *SemaphoreSuite) TestReleaseWithoutAcquirePanics(c *tc.C) {
	sem := NewProviderSemaphore(1)

	c.Assert(func() { sem.Release() }, tc.PanicMatches, ".*Release called without matching Acquire.*")
}

func (s *SemaphoreSuite) TestNegativeCapacityClampedToZero(c *tc.C) {
	sem := NewProviderSemaphore(-5)
	c.Assert(sem.Size(), tc.Equals, 0)
}

func (s *SemaphoreSuite) TestResizeNegativeClampedToZero(c *tc.C) {
	sem := NewProviderSemaphore(3)
	c.Assert(sem.Size(), tc.Equals, 3)

	sem.Resize(-10)
	c.Assert(sem.Size(), tc.Equals, 0)
}

func (s *SemaphoreSuite) TestZeroCapacityBlocksAllAcquires(c *tc.C) {
	sem := NewProviderSemaphore(0)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := sem.Acquire(ctx)
	c.Assert(err, tc.Equals, context.DeadlineExceeded)
}

func (s *SemaphoreSuite) TestMultipleReleaseAndAcquireCycles(c *tc.C) {
	sem := NewProviderSemaphore(2)
	ctx := context.Background()

	// Multiple cycles of acquire and release
	for cycle := 0; cycle < 3; cycle++ {
		// Acquire both slots
		err := sem.Acquire(ctx)
		c.Assert(err, tc.IsNil)
		err = sem.Acquire(ctx)
		c.Assert(err, tc.IsNil)
		c.Assert(sem.heldCount(), tc.Equals, 2)

		// Release both
		sem.Release()
		sem.Release()
		c.Assert(sem.heldCount(), tc.Equals, 0)
	}
}

func (s *SemaphoreSuite) TestAcquireImmediateWhenSlotsAvailable(c *tc.C) {
	sem := NewProviderSemaphore(5)
	ctx := context.Background()

	// All acquires should be immediate
	start := time.Now()
	for i := 0; i < 5; i++ {
		err := sem.Acquire(ctx)
		c.Assert(err, tc.IsNil)
	}
	elapsed := time.Since(start)

	// Should complete very quickly (no blocking)
	c.Assert(elapsed < 100*time.Millisecond, tc.IsTrue)
	c.Assert(sem.heldCount(), tc.Equals, 5)

	// Cleanup
	for i := 0; i < 5; i++ {
		sem.Release()
	}
}
