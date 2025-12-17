// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package provisionertaskbis

import (
	"context"
	"sync"
)

// providerSemaphore implements ProviderSemaphore using a mutex and condition variable.
// It supports resizing and context cancellation without goroutine leaks.
//
// Resize semantics:
// - Growing capacity immediately wakes waiters that can now be served
// - Shrinking capacity does NOT preempt in-flight holders; it only affects
//   future Acquire calls. If currently held > newSize, new Acquire calls
//   will block until enough releases bring held <= newSize.
//
// Over-Release behavior:
// - Calling Release more times than Acquire succeeded will panic.
//   This is a programming error that should be caught during development.
type providerSemaphore struct {
	mu       sync.Mutex
	cond     *sync.Cond
	size     int // configured capacity
	held     int // currently held slots
	waiters  int // number of goroutines waiting to acquire
}

// NewProviderSemaphore creates a new semaphore with the given initial capacity.
// Capacity must be >= 0; if negative, it will be clamped to 0.
func NewProviderSemaphore(capacity int) *providerSemaphore {
	if capacity < 0 {
		capacity = 0
	}
	s := &providerSemaphore{
		size: capacity,
	}
	s.cond = sync.NewCond(&s.mu)
	return s
}

// Acquire blocks until a slot is available or the context is cancelled.
// Returns nil on success, or ctx.Err() if cancelled while waiting.
//
// Acquisition is FIFO-ish in practice due to Go's runtime scheduling,
// but not strictly guaranteed. For the provisioner's use case, this is acceptable.
func (s *providerSemaphore) Acquire(ctx context.Context) error {
	s.mu.Lock()

	// Fast path: slot available and no one waiting
	if s.held < s.size && s.waiters == 0 {
		s.held++
		s.mu.Unlock()
		return nil
	}

	// Need to wait. Set up context cancellation watching.
	s.waiters++

	// Create a done channel that will be closed when context is cancelled
	done := make(chan struct{})
	cancelled := false

	go func() {
		select {
		case <-ctx.Done():
			s.mu.Lock()
			cancelled = true
			s.cond.Broadcast() // Wake up the waiting goroutine
			s.mu.Unlock()
		case <-done:
			// Acquire completed, nothing to do
		}
	}()

	// Wait for slot or cancellation
	for s.held >= s.size && !cancelled {
		s.cond.Wait()
	}

	// Signal the cancellation watcher goroutine to exit
	close(done)

	if cancelled {
		s.waiters--
		s.mu.Unlock()
		return ctx.Err()
	}

	// Got a slot
	s.held++
	s.waiters--
	s.mu.Unlock()
	return nil
}

// Release returns a held slot to the pool.
// Panics if called more times than Acquire succeeded (programming error).
func (s *providerSemaphore) Release() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.held <= 0 {
		panic("providerSemaphore: Release called without matching Acquire")
	}

	s.held--
	// Wake one waiter (if any) since a slot is now available
	s.cond.Signal()
}

// Resize changes the configured capacity.
// If n < 0, capacity is set to 0.
//
// Growing: immediately wakes waiters that can now be served.
// Shrinking: does NOT preempt in-flight holders. New Acquire calls will
// block until enough releases happen.
func (s *providerSemaphore) Resize(n int) {
	if n < 0 {
		n = 0
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	oldSize := s.size
	s.size = n

	// If we grew, wake all waiters so they can check if slots are available
	if n > oldSize {
		s.cond.Broadcast()
	}
	// If we shrank, nothing special needed - held slots remain held,
	// and future acquires will block until held <= size
}

// Size returns the currently configured capacity.
func (s *providerSemaphore) Size() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.size
}

// held returns the number of currently held slots (for testing).
func (s *providerSemaphore) heldCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.held
}

// waitersCount returns the number of goroutines waiting (for testing).
func (s *providerSemaphore) waitersCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.waiters
}
