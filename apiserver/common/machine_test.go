// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package common_test

import (
	"context"
	"time"

	"github.com/juju/errors"
	"github.com/juju/names/v5"
	"github.com/juju/naturalsort"
	jc "github.com/juju/testing/checkers"
	"go.uber.org/mock/gomock"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/apiserver/common"
	"github.com/juju/juju/core/instance"
	"github.com/juju/juju/core/machine"
	"github.com/juju/juju/core/model"
	"github.com/juju/juju/core/objectstore"
	"github.com/juju/juju/core/status"
	"github.com/juju/juju/rpc/params"
	"github.com/juju/juju/state"
)

type machineSuite struct {
	machineService *MockMachineService
}

var _ = gc.Suite(&machineSuite{})

func (s *machineSuite) setupMocks(c *gc.C) *gomock.Controller {
	ctrl := gomock.NewController(c)
	s.machineService = NewMockMachineService(ctrl)
	return ctrl
}

func (s *machineSuite) TestMachineJobFromParams(c *gc.C) {
	var tests = []struct {
		name model.MachineJob
		want state.MachineJob
		err  string
	}{{
		name: model.JobHostUnits,
		want: state.JobHostUnits,
	}, {
		name: model.JobManageModel,
		want: state.JobManageModel,
	}, {
		name: "invalid",
		want: -1,
		err:  `invalid machine job "invalid"`,
	}}
	for _, test := range tests {
		got, err := common.MachineJobFromParams(test.name)
		if err != nil {
			c.Check(err, gc.ErrorMatches, test.err)
		}
		c.Check(got, gc.Equals, test.want)
	}
}

const (
	dontWait = time.Duration(0)
)

type fakeObjectStore struct {
	objectstore.ObjectStore
}

func (s *machineSuite) TestDestroyMachines(c *gc.C) {
	st := mockState{
		machines: map[string]*fakeMachine{
			"1": {},
			"2": {destroyErr: errors.New("unit exists error")},
			"3": {life: state.Dying},
		},
	}

	err := common.MockableDestroyMachines(&st, &fakeObjectStore{}, false, dontWait, "1", "2", "3", "4")

	c.Assert(st.machines["1"].Life(), gc.Equals, state.Dying)
	c.Assert(st.machines["1"].forceDestroyCalled, jc.IsFalse)

	c.Assert(st.machines["2"].Life(), gc.Equals, state.Alive)
	c.Assert(st.machines["2"].forceDestroyCalled, jc.IsFalse)

	c.Assert(st.machines["3"].forceDestroyCalled, jc.IsFalse)
	c.Assert(st.machines["3"].destroyCalled, jc.IsFalse)

	c.Assert(err, gc.ErrorMatches, "some machines were not destroyed: unit exists error; machine 4 does not exist")
}

func (s *machineSuite) TestForceDestroyMachines(c *gc.C) {
	st := mockState{
		machines: map[string]*fakeMachine{
			"1": {},
			"2": {life: state.Dying},
		},
	}
	err := common.MockableDestroyMachines(&st, &fakeObjectStore{}, true, dontWait, "1", "2")

	c.Assert(st.machines["1"].Life(), gc.Equals, state.Dying)
	c.Assert(st.machines["1"].forceDestroyCalled, jc.IsTrue)
	c.Assert(st.machines["2"].forceDestroyCalled, jc.IsTrue)

	c.Assert(err, jc.ErrorIsNil)
}

func (s *machineSuite) TestMachineHardwareInfo(c *gc.C) {
	defer s.setupMocks(c).Finish()

	one := uint64(1)
	amd64 := "amd64"
	gig := uint64(1024)
	st := mockState{
		machines: map[string]*fakeMachine{
			"1": {id: "1", life: state.Alive, containerType: instance.NONE,
				hw: &instance.HardwareCharacteristics{
					Arch:     &amd64,
					Mem:      &gig,
					CpuCores: &one,
					CpuPower: &one,
				}},
			"2": {id: "2", life: state.Alive, containerType: instance.LXD},
			"3": {life: state.Dying},
		},
	}
	s.machineService.EXPECT().GetMachineUUID(gomock.Any(), machine.Name("1")).Return("uuid-1", nil)
	s.machineService.EXPECT().InstanceID(gomock.Any(), "uuid-1").Return("123", nil)
	s.machineService.EXPECT().GetMachineUUID(gomock.Any(), machine.Name("2")).Return("uuid-2", nil)
	s.machineService.EXPECT().InstanceID(gomock.Any(), "uuid-2").Return("456", nil)
	info, err := common.ModelMachineInfo(context.Background(), &st, s.machineService)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(info, jc.DeepEquals, []params.ModelMachineInfo{
		{
			Id:         "1",
			InstanceId: "123",
			Hardware: &params.MachineHardware{
				Arch:     &amd64,
				Mem:      &gig,
				Cores:    &one,
				CpuPower: &one,
			},
		}, {
			Id:         "2",
			InstanceId: "456",
		},
	})
}

func (s *machineSuite) TestMachineInstanceInfo(c *gc.C) {
	defer s.setupMocks(c).Finish()

	st := mockState{
		machines: map[string]*fakeMachine{
			"1": {
				id:     "1",
				instId: "123",
				status: status.Down,
			},
			"2": {
				id:     "2",
				instId: "456",
				status: status.Allocating,
			},
		},
		controllerNodes: map[string]*mockControllerNode{
			"1": {
				id:        "1",
				hasVote:   true,
				wantsVote: true,
			},
			"2": {
				id:        "2",
				hasVote:   false,
				wantsVote: true,
			},
		},
	}
	s.machineService.EXPECT().GetMachineUUID(gomock.Any(), machine.Name("1")).Return("uuid-1", nil)
	s.machineService.EXPECT().InstanceID(gomock.Any(), "uuid-1").Return("123", nil)
	s.machineService.EXPECT().GetMachineUUID(gomock.Any(), machine.Name("2")).Return("uuid-2", nil)
	s.machineService.EXPECT().InstanceID(gomock.Any(), "uuid-2").Return("456", nil)
	info, err := common.ModelMachineInfo(context.Background(), &st, s.machineService)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(info, jc.DeepEquals, []params.ModelMachineInfo{
		{
			Id:         "1",
			InstanceId: "123",
			Status:     "down",
			HasVote:    true,
			WantsVote:  true,
		},
		{
			Id:         "2",
			InstanceId: "456",
			Status:     "allocating",
			HasVote:    false,
			WantsVote:  true,
		},
	})
}

func (s *machineSuite) TestMachineInstanceInfoWithEmptyDisplayName(c *gc.C) {
	defer s.setupMocks(c).Finish()

	st := mockState{
		machines: map[string]*fakeMachine{
			"1": {
				id:     "1",
				instId: "123",
				status: status.Down,
			},
		},
		controllerNodes: map[string]*mockControllerNode{
			"1": {
				id:        "1",
				hasVote:   true,
				wantsVote: true,
			},
		},
	}
	s.machineService.EXPECT().GetMachineUUID(gomock.Any(), machine.Name("1")).Return("uuid-1", nil)
	s.machineService.EXPECT().InstanceID(gomock.Any(), "uuid-1").Return("123", nil)
	info, err := common.ModelMachineInfo(context.Background(), &st, s.machineService)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(info, jc.DeepEquals, []params.ModelMachineInfo{
		{
			Id:         "1",
			InstanceId: "123",
			Status:     "down",
			HasVote:    true,
			WantsVote:  true,
		},
	})
}

func (s *machineSuite) TestMachineInstanceInfoWithSetDisplayName(c *gc.C) {
	defer s.setupMocks(c).Finish()

	st := mockState{
		machines: map[string]*fakeMachine{
			"1": {
				id:     "1",
				instId: "123",
				status: status.Down,
			},
		},
		controllerNodes: map[string]*mockControllerNode{
			"1": {
				id:        "1",
				hasVote:   true,
				wantsVote: true,
			},
		},
	}
	s.machineService.EXPECT().GetMachineUUID(gomock.Any(), machine.Name("1")).Return("uuid-1", nil)
	s.machineService.EXPECT().InstanceID(gomock.Any(), "uuid-1").Return("123", nil)
	info, err := common.ModelMachineInfo(context.Background(), &st, s.machineService)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(info, jc.DeepEquals, []params.ModelMachineInfo{
		{
			Id:         "1",
			InstanceId: "123",
			Status:     "down",
			HasVote:    true,
			WantsVote:  true,
		},
	})
}

func (s *machineSuite) TestMachineInstanceInfoWithHAPrimary(c *gc.C) {
	defer s.setupMocks(c).Finish()

	st := mockState{
		machines: map[string]*fakeMachine{
			"1": {
				id:     "1",
				instId: "123",
				status: status.Down,
			},
		},
		controllerNodes: map[string]*mockControllerNode{
			"1": {
				id:        "1",
				hasVote:   true,
				wantsVote: true,
			},
			"2": {
				id:        "1",
				hasVote:   true,
				wantsVote: true,
			},
		},
		haPrimaryMachineF: func() (names.MachineTag, error) {
			return names.NewMachineTag("1"), nil
		},
	}
	s.machineService.EXPECT().GetMachineUUID(gomock.Any(), machine.Name("1")).Return("uuid-1", nil)
	s.machineService.EXPECT().InstanceID(gomock.Any(), "uuid-1").Return("123", nil)
	info, err := common.ModelMachineInfo(context.Background(), &st, s.machineService)
	c.Assert(err, jc.ErrorIsNil)
	_true := true
	c.Assert(info, jc.DeepEquals, []params.ModelMachineInfo{
		{
			Id:         "1",
			InstanceId: "123",
			Status:     "down",
			HasVote:    true,
			WantsVote:  true,
			HAPrimary:  &_true,
		},
	})
}

type mockState struct {
	common.ModelManagerBackend
	machines          map[string]*fakeMachine
	controllerNodes   map[string]*mockControllerNode
	haPrimaryMachineF func() (names.MachineTag, error)
}

func (st *mockState) Machine(id string) (common.Machine, error) {
	if m, ok := st.machines[id]; ok {
		return m, nil
	}
	return nil, errors.Errorf("machine %s does not exist", id)
}

func (st *mockState) AllMachines() (machines []common.Machine, _ error) {
	// Ensure we get machines in id order.
	var ids []string
	for id := range st.machines {
		ids = append(ids, id)
	}
	naturalsort.Sort(ids)
	for _, id := range ids {
		machines = append(machines, st.machines[id])
	}
	return machines, nil
}

func (st *mockState) ControllerNodes() ([]common.ControllerNode, error) {
	var result []common.ControllerNode
	for _, n := range st.controllerNodes {
		result = append(result, n)
	}
	return result, nil
}

func (st *mockState) HAPrimaryMachine() (names.MachineTag, error) {
	if st.haPrimaryMachineF == nil {
		return names.MachineTag{}, nil
	}
	return st.haPrimaryMachineF()
}

type mockControllerNode struct {
	id        string
	hasVote   bool
	wantsVote bool
}

func (m *mockControllerNode) Id() string {
	return m.id
}

func (m *mockControllerNode) WantsVote() bool {
	return m.wantsVote
}

func (m *mockControllerNode) HasVote() bool {
	return m.hasVote
}

type fakeMachine struct {
	state.Machine
	id                 string
	life               state.Life
	containerType      instance.ContainerType
	hw                 *instance.HardwareCharacteristics
	instId             instance.Id
	displayName        string
	status             status.Status
	statusErr          error
	destroyErr         error
	forceDestroyErr    error
	forceDestroyCalled bool
	destroyCalled      bool
}

func (m *fakeMachine) Id() string {
	return m.id
}

func (m *fakeMachine) Life() state.Life {
	return m.life
}

func (m *fakeMachine) InstanceId() (instance.Id, error) {
	return m.instId, nil
}

func (m *fakeMachine) InstanceNames() (instance.Id, string, error) {
	instId, err := m.InstanceId()
	return instId, m.displayName, err
}

func (m *fakeMachine) Status() (status.StatusInfo, error) {
	return status.StatusInfo{
		Status: m.status,
	}, m.statusErr
}

func (m *fakeMachine) HardwareCharacteristics() (*instance.HardwareCharacteristics, error) {
	return m.hw, nil
}

func (m *fakeMachine) ForceDestroy(time.Duration) error {
	m.forceDestroyCalled = true
	if m.forceDestroyErr != nil {
		return m.forceDestroyErr
	}
	m.life = state.Dying
	return nil
}

func (m *fakeMachine) Destroy(_ objectstore.ObjectStore) error {
	m.destroyCalled = true
	if m.destroyErr != nil {
		return m.destroyErr
	}
	m.life = state.Dying
	return nil
}
