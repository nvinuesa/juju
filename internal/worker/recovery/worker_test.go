// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package recovery

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/juju/tc"

	internalrecovery "github.com/juju/juju/internal/recovery"
)

type workerSuite struct{}

func TestWorkerSuite(t *testing.T) { tc.Run(t, &workerSuite{}) }

type completion struct {
	calls    int
	unlocked bool
	err      error
}

func (f *completion) SetFlag(context.Context, string, bool, string) error { f.calls++; return f.err }
func (f *completion) Unlock()                                             { f.unlocked = true }

func (s *workerSuite) TestCompletionAndRestart(c *tc.C) {
	dir, sum := c.MkDir(), strings.Repeat("a", 64)
	c.Assert(internalrecovery.WriteMarker(dir, internalrecovery.InitialisedFile, sum), tc.ErrorIsNil)
	f := &completion{}
	runs := 0
	cfg := WorkerConfig{DataDir: dir, Checksum: sum, Operation: func(context.Context) error { runs++; return nil }, FlagService: f, Unlocker: f}
	w, err := NewWorker(cfg)
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(w.Wait(), tc.ErrorIsNil)
	c.Check(runs, tc.Equals, 1)
	c.Check(f.calls, tc.Equals, 1)
	c.Check(f.unlocked, tc.IsTrue)
	f.unlocked = false
	w, err = NewWorker(cfg)
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(w.Wait(), tc.ErrorIsNil)
	c.Check(runs, tc.Equals, 1)
	c.Check(f.unlocked, tc.IsTrue)
}

func (s *workerSuite) TestFailureNeverOpensGateAndCanRetry(c *tc.C) {
	for _, operationFails := range []bool{true, false} {
		dir, sum := c.MkDir(), strings.Repeat("a", 64)
		c.Assert(internalrecovery.WriteMarker(dir, internalrecovery.InitialisedFile, sum), tc.ErrorIsNil)
		failure := errors.New("failed")
		f := &completion{}
		if !operationFails {
			f.err = failure
		}
		cfg := WorkerConfig{DataDir: dir, Checksum: sum, FlagService: f, Unlocker: f,
			Operation: func(context.Context) error {
				if operationFails {
					return failure
				}
				return nil
			}}
		w, err := NewWorker(cfg)
		c.Assert(err, tc.ErrorIsNil)
		c.Check(w.Wait(), tc.ErrorIs, failure)
		c.Check(f.unlocked, tc.IsFalse)
		done, err := internalrecovery.HasMarker(dir, internalrecovery.CompletedFile, sum)
		c.Assert(err, tc.ErrorIsNil)
		c.Check(done, tc.IsFalse)
		f.err = nil
		cfg.Operation = func(context.Context) error { return nil }
		w, err = NewWorker(cfg)
		c.Assert(err, tc.ErrorIsNil)
		c.Assert(w.Wait(), tc.ErrorIsNil)
		c.Check(f.unlocked, tc.IsTrue)
	}
}

func (s *workerSuite) TestNoImportMarker(c *tc.C) {
	f := &completion{}
	w, err := NewWorker(WorkerConfig{DataDir: c.MkDir(), Checksum: strings.Repeat("a", 64), FlagService: f, Unlocker: f,
		Operation: func(context.Context) error { c.Fatal("operation ran before import completed"); return nil }})
	c.Assert(err, tc.ErrorIsNil)
	c.Check(w.Wait(), tc.ErrorMatches, "recovery database initialisation has not completed")
	c.Check(f.unlocked, tc.IsFalse)
}

func (s *workerSuite) TestCancellation(c *tc.C) {
	dir, sum := c.MkDir(), strings.Repeat("a", 64)
	c.Assert(internalrecovery.WriteMarker(dir, internalrecovery.InitialisedFile, sum), tc.ErrorIsNil)
	started := make(chan struct{})
	f := &completion{}
	w, err := NewWorker(WorkerConfig{DataDir: dir, Checksum: sum, FlagService: f, Unlocker: f,
		Operation: func(ctx context.Context) error { close(started); <-ctx.Done(); return ctx.Err() }})
	c.Assert(err, tc.ErrorIsNil)
	<-started
	w.Kill()
	_ = w.Wait()
	c.Check(f.unlocked, tc.IsFalse)
}
