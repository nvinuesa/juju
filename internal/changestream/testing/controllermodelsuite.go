// Copyright 2023 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package testing

import (
	"time"

	"github.com/juju/tc"

	"github.com/juju/juju/core/changestream"
	coredatabase "github.com/juju/juju/core/database"
	"github.com/juju/juju/domain/schema/testing"
	"github.com/juju/juju/internal/errors"
	jujutesting "github.com/juju/juju/internal/testing"
)

// ControllerModelSuite is used to provide a sql.DB reference to tests.
// It is pre-populated with the controller schema.
type ControllerModelSuite struct {
	testing.ControllerModelSuite

	watchableDBs map[string]*TestWatchableDB
}

// SetUpTest is responsible for setting up a testing database suite initialised
// with the controller schema.
func (s *ControllerModelSuite) SetUpTest(c *tc.C) {
	s.ControllerModelSuite.SetUpTest(c)

	s.watchableDBs = map[string]*TestWatchableDB{
		coredatabase.ControllerNS: NewTestWatchableDB(c, coredatabase.ControllerNS, s.TxnRunner()),
	}
}

func (s *ControllerModelSuite) TearDownTest(c *tc.C) {
	for _, watchableDB := range s.watchableDBs {
		// We could use workertest.DirtyKill here, but some workers are already
		// dead when we get here and it causes unwanted logs. This just ensures
		// that we don't have any addition workers running.
		killAndWait(c, watchableDB)
	}
	s.ControllerModelSuite.TearDownTest(c)
}

// InitWatchableDB ensures there is a TestWatchableDB for the given namespace.
func (s *ControllerModelSuite) InitWatchableDB(c *tc.C, namespace string) (*TestWatchableDB, *Idler) {
	if watchableDB, ok := s.watchableDBs[namespace]; ok {
		return watchableDB, &Idler{watchableDB: watchableDB}
	}
	watchableDB := NewTestWatchableDB(c, namespace, s.ModelTxnRunner(c, namespace))
	s.watchableDBs[namespace] = watchableDB
	return watchableDB, &Idler{watchableDB: watchableDB}
}

// GetWatchableDB allows the ControllerModelSuite to be a WatchableDBGetter
func (s *ControllerModelSuite) GetWatchableDB(namespace string) (changestream.WatchableDB, error) {
	if watchableDB, ok := s.watchableDBs[namespace]; ok {
		return watchableDB, nil
	}
	return nil, errors.Errorf("no test watchable db for %q", namespace)
}

// Idler implements AssertChangeStreamIdle for a TestWatchableDB.
type Idler struct {
	watchableDB *TestWatchableDB
}

// AssertChangeStreamIdle returns if and when the change stream is idle.
// This is useful to ensure that the change stream is not processing any
// events before running a test.
func (idler *Idler) AssertChangeStreamIdle(c *tc.C) {
	timeout := time.After(jujutesting.LongWait)
	for {
		select {
		case state := <-idler.watchableDB.states:
			if state == stateIdle {
				return
			}
		case <-timeout:
			c.Fatalf("timed out waiting for idle state")
		}
	}
}
