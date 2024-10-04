// Copyright 2024 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package machine_test

import (
	"context"

	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/core/changestream"
	"github.com/juju/juju/core/database"
	"github.com/juju/juju/core/instance"
	"github.com/juju/juju/core/watcher/watchertest"
	"github.com/juju/juju/domain"
	"github.com/juju/juju/domain/life"
	"github.com/juju/juju/domain/machine/service"
	"github.com/juju/juju/domain/machine/state"
	changestreamtesting "github.com/juju/juju/internal/changestream/testing"
	loggertesting "github.com/juju/juju/internal/logger/testing"
)

type watcherSuite struct {
	changestreamtesting.ModelSuite

	svc *service.WatchableService
}

var _ = gc.Suite(&watcherSuite{})

func (s *watcherSuite) SetUpTest(c *gc.C) {
	s.ModelSuite.SetUpTest(c)

	factory := changestream.NewWatchableDBFactoryForNamespace(s.GetWatchableDB, "machine")
	s.svc = service.NewWatchableService(
		state.NewState(
			func() (database.TxnRunner, error) { return factory() },
			loggertesting.WrapCheckLog(c),
		),
		domain.NewWatcherFactory(factory, loggertesting.WrapCheckLog(c)),
		nil,
	)
}

func (s *watcherSuite) TestWatchModelMachines(c *gc.C) {
	_, err := s.svc.CreateMachine(context.Background(), "0")
	c.Assert(err, gc.IsNil)

	_, err = s.svc.CreateMachine(context.Background(), "0/lxd/0")
	c.Assert(err, gc.IsNil)

	s.AssertChangeStreamIdle(c)

	watcher, err := s.svc.WatchModelMachines()
	c.Assert(err, gc.IsNil)
	defer watchertest.CleanKill(c, watcher)

	watcherC := watchertest.NewStringsWatcherC(c, watcher)

	// The initial event should have the machine we created prior,
	// but not the container.
	watcherC.AssertChange("0")

	// A new machine triggers an emission.
	_, err = s.svc.CreateMachine(context.Background(), "1")
	c.Assert(err, gc.IsNil)
	watcherC.AssertChange("1")

	// An update triggers an emission.
	err = s.svc.SetMachineLife(context.Background(), "1", life.Dying)
	c.Assert(err, gc.IsNil)
	watcherC.AssertChange("1")

	// A deletion is ignored.
	err = s.svc.DeleteMachine(context.Background(), "1")
	c.Assert(err, gc.IsNil)

	// As is a container creation.
	_, err = s.svc.CreateMachine(context.Background(), "0/lxd/1")
	c.Assert(err, gc.IsNil)

	s.AssertChangeStreamIdle(c)
	watcherC.AssertNoChange()
}

func (s *watcherSuite) TestMachineCloudInstanceWatchWithSet(c *gc.C) {
	// Create a machineUUID and set its cloud instance.
	machineUUID, err := s.svc.CreateMachine(context.Background(), "machine-1")
	c.Assert(err, gc.IsNil)
	hc := &instance.HardwareCharacteristics{
		Mem:      uintptr(1024),
		RootDisk: uintptr(256),
		CpuCores: uintptr(4),
		CpuPower: uintptr(75),
	}
	watcher, err := s.svc.WatchMachineCloudInstances(context.Background(), machineUUID)
	c.Assert(err, jc.ErrorIsNil)
	harness := watchertest.NewHarness(s, watchertest.NewWatcherC(c, watcher))

	// Should notify when the machine cloud instance is set.
	harness.AddTest(func(c *gc.C) {
		err = s.svc.SetMachineCloudInstance(context.Background(), machineUUID, "42", "", hc)
		c.Assert(err, jc.ErrorIsNil)
	}, func(w watchertest.WatcherC[struct{}]) {
		w.Check(watchertest.SliceAssert(struct{}{}))
	})
}

func (s *watcherSuite) TestMachineCloudInstanceWatchWithDelete(c *gc.C) {
	// Create a machineUUID and set its cloud instance.
	machineUUID, err := s.svc.CreateMachine(context.Background(), "machine-1")
	c.Assert(err, gc.IsNil)
	hc := &instance.HardwareCharacteristics{
		Mem:      uintptr(1024),
		RootDisk: uintptr(256),
		CpuCores: uintptr(4),
		CpuPower: uintptr(75),
	}
	err = s.svc.SetMachineCloudInstance(context.Background(), machineUUID, "42", "", hc)
	c.Assert(err, gc.IsNil)

	watcher, err := s.svc.WatchMachineCloudInstances(context.Background(), machineUUID)
	c.Assert(err, jc.ErrorIsNil)
	harness := watchertest.NewHarness(s, watchertest.NewWatcherC(c, watcher))

	// Should notify when the machine cloud instance is deleted.
	harness.AddTest(func(c *gc.C) {
		err = s.svc.DeleteMachineCloudInstance(context.Background(), machineUUID)
		c.Assert(err, jc.ErrorIsNil)
	}, func(w watchertest.WatcherC[struct{}]) {
		w.Check(watchertest.SliceAssert(struct{}{}))
	})
}

func (s *watcherSuite) TestWatchLXDProfiles(c *gc.C) {
	machineUUIDm0, err := s.svc.CreateMachine(context.Background(), "machine-1")
	c.Assert(err, jc.ErrorIsNil)
	machineUUIDm1, err := s.svc.CreateMachine(context.Background(), "machine-2")
	c.Assert(err, jc.ErrorIsNil)

	watcher, err := s.svc.WatchLXDProfiles(context.Background(), machineUUIDm0)
	c.Assert(err, jc.ErrorIsNil)
	harness := watchertest.NewHarness(s, watchertest.NewWatcherC(c, watcher))

	// Should notify when a new profile is added.
	harness.AddTest(func(c *gc.C) {
		err := s.svc.SetAppliedLXDProfileNames(context.Background(), machineUUIDm0, []string{"profile-0"})
		c.Assert(err, jc.ErrorIsNil)
	}, func(w watchertest.WatcherC[struct{}]) {
		w.Check(watchertest.SliceAssert(struct{}{}))
	})

	// Should notify when profiles are overwritten.
	harness.AddTest(func(c *gc.C) {
		err := s.svc.SetAppliedLXDProfileNames(context.Background(), machineUUIDm0, []string{"profile-0", "profile-1", "profile-2"})
		c.Assert(err, jc.ErrorIsNil)
	}, func(w watchertest.WatcherC[struct{}]) {
		w.Check(watchertest.SliceAssert(struct{}{}))
	})

	// Nothing to notify when the lxd profiles are set on the other (non
	// watched) machine.
	harness.AddTest(func(c *gc.C) {
		err := s.svc.SetAppliedLXDProfileNames(context.Background(), machineUUIDm1, []string{"profile-0"})
		c.Assert(err, jc.ErrorIsNil)
	}, func(w watchertest.WatcherC[struct{}]) {
		w.AssertNoChange()
	})

	harness.Run(c)
}

// TestWatchMachineForReboot tests the functionality of watching machines for reboot.
// It creates a machine hierarchy with a parent, a child (which will be watched), and a control child.
// Then it creates a watcher for the child and performs the following assertions:
// - The watcher is not notified when a sibling is asked for reboot.
// - The watcher is notified when the child is directly asked for reboot.
// - The watcher is notified when the parent is required for reboot.
// The tests are run using the watchertest harness.
func (s *watcherSuite) TestWatchMachineForReboot(c *gc.C) {
	// Create machine hierarchy to reboot from parent, with a child (which will be watched) and a control child
	parentUUID, err := s.svc.CreateMachine(context.Background(), "parent")
	c.Assert(err, gc.IsNil)
	childUUID, err := s.svc.CreateMachineWithParent(context.Background(), "child", "parent")
	c.Assert(err, jc.ErrorIsNil)
	controlUUID, err := s.svc.CreateMachineWithParent(context.Background(), "control", "parent")
	c.Assert(err, jc.ErrorIsNil)

	// Create watcher for child
	watcher, err := s.svc.WatchMachineReboot(context.Background(), childUUID)
	c.Assert(err, gc.IsNil)

	harness := watchertest.NewHarness(s, watchertest.NewWatcherC(c, watcher))

	// Ensure that the watcher is not notified when a sibling is asked for reboot
	harness.AddTest(func(c *gc.C) {
		err := s.svc.RequireMachineReboot(context.Background(), controlUUID)
		c.Assert(err, jc.ErrorIsNil)
	}, func(w watchertest.WatcherC[struct{}]) {
		w.AssertNoChange()
	})

	// Ensure that the watcher is notified when the child is directly asked for reboot
	harness.AddTest(func(c *gc.C) {
		err := s.svc.RequireMachineReboot(context.Background(), childUUID)
		c.Assert(err, jc.ErrorIsNil)
	}, func(w watchertest.WatcherC[struct{}]) {
		w.Check(watchertest.SliceAssert(struct{}{}))
	})

	// Ensure that the watcher is notified when the parent is required for reboot
	harness.AddTest(func(c *gc.C) {
		err := s.svc.RequireMachineReboot(context.Background(), parentUUID)
		c.Assert(err, jc.ErrorIsNil)
	}, func(w watchertest.WatcherC[struct{}]) {
		w.Check(watchertest.SliceAssert(struct{}{}))
	})

	// Ensure that the watcher is not notified when a sibling is cleared from reboot
	harness.AddTest(func(c *gc.C) {
		err := s.svc.ClearMachineReboot(context.Background(), controlUUID)
		c.Assert(err, jc.ErrorIsNil)
	}, func(w watchertest.WatcherC[struct{}]) {
		w.AssertNoChange()
	})

	// Ensure that the watcher is notified when the child is directly cleared from reboot
	harness.AddTest(func(c *gc.C) {
		err := s.svc.ClearMachineReboot(context.Background(), childUUID)
		c.Assert(err, jc.ErrorIsNil)
	}, func(w watchertest.WatcherC[struct{}]) {
		w.Check(watchertest.SliceAssert(struct{}{}))
	})

	// Ensure that the watcher is notified when the parent is cleared from reboot
	harness.AddTest(func(c *gc.C) {
		err := s.svc.ClearMachineReboot(context.Background(), parentUUID)
		c.Assert(err, jc.ErrorIsNil)
	}, func(w watchertest.WatcherC[struct{}]) {
		w.Check(watchertest.SliceAssert(struct{}{}))
	})

	harness.Run(c)
}

func uintptr(u uint64) *uint64 {
	return &u
}
