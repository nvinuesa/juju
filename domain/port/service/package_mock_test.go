// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/juju/juju/domain/port/service (interfaces: State)
//
// Generated by this command:
//
//	mockgen -typed -package service -destination package_mock_test.go github.com/juju/juju/domain/port/service State
//

// Package service is a generated GoMock package.
package service

import (
	context "context"
	reflect "reflect"

	set "github.com/juju/collections/set"
	application "github.com/juju/juju/core/application"
	machine "github.com/juju/juju/core/machine"
	network "github.com/juju/juju/core/network"
	unit "github.com/juju/juju/core/unit"
	domain "github.com/juju/juju/domain"
	port "github.com/juju/juju/domain/port"
	gomock "go.uber.org/mock/gomock"
)

// MockState is a mock of State interface.
type MockState struct {
	ctrl     *gomock.Controller
	recorder *MockStateMockRecorder
}

// MockStateMockRecorder is the mock recorder for MockState.
type MockStateMockRecorder struct {
	mock *MockState
}

// NewMockState creates a new mock instance.
func NewMockState(ctrl *gomock.Controller) *MockState {
	mock := &MockState{ctrl: ctrl}
	mock.recorder = &MockStateMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockState) EXPECT() *MockStateMockRecorder {
	return m.recorder
}

// FilterEndpointsForApplication mocks base method.
func (m *MockState) FilterEndpointsForApplication(arg0 context.Context, arg1 application.ID, arg2 []string) (set.Strings, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "FilterEndpointsForApplication", arg0, arg1, arg2)
	ret0, _ := ret[0].(set.Strings)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// FilterEndpointsForApplication indicates an expected call of FilterEndpointsForApplication.
func (mr *MockStateMockRecorder) FilterEndpointsForApplication(arg0, arg1, arg2 any) *MockStateFilterEndpointsForApplicationCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "FilterEndpointsForApplication", reflect.TypeOf((*MockState)(nil).FilterEndpointsForApplication), arg0, arg1, arg2)
	return &MockStateFilterEndpointsForApplicationCall{Call: call}
}

// MockStateFilterEndpointsForApplicationCall wrap *gomock.Call
type MockStateFilterEndpointsForApplicationCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockStateFilterEndpointsForApplicationCall) Return(arg0 set.Strings, arg1 error) *MockStateFilterEndpointsForApplicationCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockStateFilterEndpointsForApplicationCall) Do(f func(context.Context, application.ID, []string) (set.Strings, error)) *MockStateFilterEndpointsForApplicationCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockStateFilterEndpointsForApplicationCall) DoAndReturn(f func(context.Context, application.ID, []string) (set.Strings, error)) *MockStateFilterEndpointsForApplicationCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// GetAllOpenedPorts mocks base method.
func (m *MockState) GetAllOpenedPorts(arg0 context.Context) (network.GroupedPortRanges, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetAllOpenedPorts", arg0)
	ret0, _ := ret[0].(network.GroupedPortRanges)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetAllOpenedPorts indicates an expected call of GetAllOpenedPorts.
func (mr *MockStateMockRecorder) GetAllOpenedPorts(arg0 any) *MockStateGetAllOpenedPortsCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetAllOpenedPorts", reflect.TypeOf((*MockState)(nil).GetAllOpenedPorts), arg0)
	return &MockStateGetAllOpenedPortsCall{Call: call}
}

// MockStateGetAllOpenedPortsCall wrap *gomock.Call
type MockStateGetAllOpenedPortsCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockStateGetAllOpenedPortsCall) Return(arg0 network.GroupedPortRanges, arg1 error) *MockStateGetAllOpenedPortsCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockStateGetAllOpenedPortsCall) Do(f func(context.Context) (network.GroupedPortRanges, error)) *MockStateGetAllOpenedPortsCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockStateGetAllOpenedPortsCall) DoAndReturn(f func(context.Context) (network.GroupedPortRanges, error)) *MockStateGetAllOpenedPortsCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// GetApplicationOpenedPorts mocks base method.
func (m *MockState) GetApplicationOpenedPorts(arg0 context.Context, arg1 application.ID) (port.UnitEndpointPortRanges, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetApplicationOpenedPorts", arg0, arg1)
	ret0, _ := ret[0].(port.UnitEndpointPortRanges)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetApplicationOpenedPorts indicates an expected call of GetApplicationOpenedPorts.
func (mr *MockStateMockRecorder) GetApplicationOpenedPorts(arg0, arg1 any) *MockStateGetApplicationOpenedPortsCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetApplicationOpenedPorts", reflect.TypeOf((*MockState)(nil).GetApplicationOpenedPorts), arg0, arg1)
	return &MockStateGetApplicationOpenedPortsCall{Call: call}
}

// MockStateGetApplicationOpenedPortsCall wrap *gomock.Call
type MockStateGetApplicationOpenedPortsCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockStateGetApplicationOpenedPortsCall) Return(arg0 port.UnitEndpointPortRanges, arg1 error) *MockStateGetApplicationOpenedPortsCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockStateGetApplicationOpenedPortsCall) Do(f func(context.Context, application.ID) (port.UnitEndpointPortRanges, error)) *MockStateGetApplicationOpenedPortsCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockStateGetApplicationOpenedPortsCall) DoAndReturn(f func(context.Context, application.ID) (port.UnitEndpointPortRanges, error)) *MockStateGetApplicationOpenedPortsCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// GetColocatedOpenedPorts mocks base method.
func (m *MockState) GetColocatedOpenedPorts(arg0 domain.AtomicContext, arg1 unit.UUID) ([]network.PortRange, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetColocatedOpenedPorts", arg0, arg1)
	ret0, _ := ret[0].([]network.PortRange)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetColocatedOpenedPorts indicates an expected call of GetColocatedOpenedPorts.
func (mr *MockStateMockRecorder) GetColocatedOpenedPorts(arg0, arg1 any) *MockStateGetColocatedOpenedPortsCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetColocatedOpenedPorts", reflect.TypeOf((*MockState)(nil).GetColocatedOpenedPorts), arg0, arg1)
	return &MockStateGetColocatedOpenedPortsCall{Call: call}
}

// MockStateGetColocatedOpenedPortsCall wrap *gomock.Call
type MockStateGetColocatedOpenedPortsCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockStateGetColocatedOpenedPortsCall) Return(arg0 []network.PortRange, arg1 error) *MockStateGetColocatedOpenedPortsCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockStateGetColocatedOpenedPortsCall) Do(f func(domain.AtomicContext, unit.UUID) ([]network.PortRange, error)) *MockStateGetColocatedOpenedPortsCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockStateGetColocatedOpenedPortsCall) DoAndReturn(f func(domain.AtomicContext, unit.UUID) ([]network.PortRange, error)) *MockStateGetColocatedOpenedPortsCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// GetEndpointOpenedPorts mocks base method.
func (m *MockState) GetEndpointOpenedPorts(arg0 domain.AtomicContext, arg1 unit.UUID, arg2 string) ([]network.PortRange, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetEndpointOpenedPorts", arg0, arg1, arg2)
	ret0, _ := ret[0].([]network.PortRange)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetEndpointOpenedPorts indicates an expected call of GetEndpointOpenedPorts.
func (mr *MockStateMockRecorder) GetEndpointOpenedPorts(arg0, arg1, arg2 any) *MockStateGetEndpointOpenedPortsCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetEndpointOpenedPorts", reflect.TypeOf((*MockState)(nil).GetEndpointOpenedPorts), arg0, arg1, arg2)
	return &MockStateGetEndpointOpenedPortsCall{Call: call}
}

// MockStateGetEndpointOpenedPortsCall wrap *gomock.Call
type MockStateGetEndpointOpenedPortsCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockStateGetEndpointOpenedPortsCall) Return(arg0 []network.PortRange, arg1 error) *MockStateGetEndpointOpenedPortsCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockStateGetEndpointOpenedPortsCall) Do(f func(domain.AtomicContext, unit.UUID, string) ([]network.PortRange, error)) *MockStateGetEndpointOpenedPortsCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockStateGetEndpointOpenedPortsCall) DoAndReturn(f func(domain.AtomicContext, unit.UUID, string) ([]network.PortRange, error)) *MockStateGetEndpointOpenedPortsCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// GetEndpoints mocks base method.
func (m *MockState) GetEndpoints(arg0 domain.AtomicContext, arg1 unit.UUID) ([]string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetEndpoints", arg0, arg1)
	ret0, _ := ret[0].([]string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetEndpoints indicates an expected call of GetEndpoints.
func (mr *MockStateMockRecorder) GetEndpoints(arg0, arg1 any) *MockStateGetEndpointsCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetEndpoints", reflect.TypeOf((*MockState)(nil).GetEndpoints), arg0, arg1)
	return &MockStateGetEndpointsCall{Call: call}
}

// MockStateGetEndpointsCall wrap *gomock.Call
type MockStateGetEndpointsCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockStateGetEndpointsCall) Return(arg0 []string, arg1 error) *MockStateGetEndpointsCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockStateGetEndpointsCall) Do(f func(domain.AtomicContext, unit.UUID) ([]string, error)) *MockStateGetEndpointsCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockStateGetEndpointsCall) DoAndReturn(f func(domain.AtomicContext, unit.UUID) ([]string, error)) *MockStateGetEndpointsCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// GetMachineNamesForEndpoints mocks base method.
func (m *MockState) GetMachineNamesForEndpoints(arg0 context.Context, arg1 []string) ([]machine.Name, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetMachineNamesForEndpoints", arg0, arg1)
	ret0, _ := ret[0].([]machine.Name)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetMachineNamesForEndpoints indicates an expected call of GetMachineNamesForEndpoints.
func (mr *MockStateMockRecorder) GetMachineNamesForEndpoints(arg0, arg1 any) *MockStateGetMachineNamesForEndpointsCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetMachineNamesForEndpoints", reflect.TypeOf((*MockState)(nil).GetMachineNamesForEndpoints), arg0, arg1)
	return &MockStateGetMachineNamesForEndpointsCall{Call: call}
}

// MockStateGetMachineNamesForEndpointsCall wrap *gomock.Call
type MockStateGetMachineNamesForEndpointsCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockStateGetMachineNamesForEndpointsCall) Return(arg0 []machine.Name, arg1 error) *MockStateGetMachineNamesForEndpointsCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockStateGetMachineNamesForEndpointsCall) Do(f func(context.Context, []string) ([]machine.Name, error)) *MockStateGetMachineNamesForEndpointsCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockStateGetMachineNamesForEndpointsCall) DoAndReturn(f func(context.Context, []string) ([]machine.Name, error)) *MockStateGetMachineNamesForEndpointsCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// GetMachineOpenedPorts mocks base method.
func (m *MockState) GetMachineOpenedPorts(arg0 context.Context, arg1 string) (map[string]network.GroupedPortRanges, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetMachineOpenedPorts", arg0, arg1)
	ret0, _ := ret[0].(map[string]network.GroupedPortRanges)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetMachineOpenedPorts indicates an expected call of GetMachineOpenedPorts.
func (mr *MockStateMockRecorder) GetMachineOpenedPorts(arg0, arg1 any) *MockStateGetMachineOpenedPortsCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetMachineOpenedPorts", reflect.TypeOf((*MockState)(nil).GetMachineOpenedPorts), arg0, arg1)
	return &MockStateGetMachineOpenedPortsCall{Call: call}
}

// MockStateGetMachineOpenedPortsCall wrap *gomock.Call
type MockStateGetMachineOpenedPortsCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockStateGetMachineOpenedPortsCall) Return(arg0 map[string]network.GroupedPortRanges, arg1 error) *MockStateGetMachineOpenedPortsCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockStateGetMachineOpenedPortsCall) Do(f func(context.Context, string) (map[string]network.GroupedPortRanges, error)) *MockStateGetMachineOpenedPortsCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockStateGetMachineOpenedPortsCall) DoAndReturn(f func(context.Context, string) (map[string]network.GroupedPortRanges, error)) *MockStateGetMachineOpenedPortsCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// GetUnitOpenedPorts mocks base method.
func (m *MockState) GetUnitOpenedPorts(arg0 context.Context, arg1 unit.UUID) (network.GroupedPortRanges, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetUnitOpenedPorts", arg0, arg1)
	ret0, _ := ret[0].(network.GroupedPortRanges)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetUnitOpenedPorts indicates an expected call of GetUnitOpenedPorts.
func (mr *MockStateMockRecorder) GetUnitOpenedPorts(arg0, arg1 any) *MockStateGetUnitOpenedPortsCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetUnitOpenedPorts", reflect.TypeOf((*MockState)(nil).GetUnitOpenedPorts), arg0, arg1)
	return &MockStateGetUnitOpenedPortsCall{Call: call}
}

// MockStateGetUnitOpenedPortsCall wrap *gomock.Call
type MockStateGetUnitOpenedPortsCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockStateGetUnitOpenedPortsCall) Return(arg0 network.GroupedPortRanges, arg1 error) *MockStateGetUnitOpenedPortsCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockStateGetUnitOpenedPortsCall) Do(f func(context.Context, unit.UUID) (network.GroupedPortRanges, error)) *MockStateGetUnitOpenedPortsCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockStateGetUnitOpenedPortsCall) DoAndReturn(f func(context.Context, unit.UUID) (network.GroupedPortRanges, error)) *MockStateGetUnitOpenedPortsCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// InitialWatchOpenedPortsStatement mocks base method.
func (m *MockState) InitialWatchOpenedPortsStatement() string {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "InitialWatchOpenedPortsStatement")
	ret0, _ := ret[0].(string)
	return ret0
}

// InitialWatchOpenedPortsStatement indicates an expected call of InitialWatchOpenedPortsStatement.
func (mr *MockStateMockRecorder) InitialWatchOpenedPortsStatement() *MockStateInitialWatchOpenedPortsStatementCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "InitialWatchOpenedPortsStatement", reflect.TypeOf((*MockState)(nil).InitialWatchOpenedPortsStatement))
	return &MockStateInitialWatchOpenedPortsStatementCall{Call: call}
}

// MockStateInitialWatchOpenedPortsStatementCall wrap *gomock.Call
type MockStateInitialWatchOpenedPortsStatementCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockStateInitialWatchOpenedPortsStatementCall) Return(arg0 string) *MockStateInitialWatchOpenedPortsStatementCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockStateInitialWatchOpenedPortsStatementCall) Do(f func() string) *MockStateInitialWatchOpenedPortsStatementCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockStateInitialWatchOpenedPortsStatementCall) DoAndReturn(f func() string) *MockStateInitialWatchOpenedPortsStatementCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// RunAtomic mocks base method.
func (m *MockState) RunAtomic(arg0 context.Context, arg1 func(domain.AtomicContext) error) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "RunAtomic", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// RunAtomic indicates an expected call of RunAtomic.
func (mr *MockStateMockRecorder) RunAtomic(arg0, arg1 any) *MockStateRunAtomicCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RunAtomic", reflect.TypeOf((*MockState)(nil).RunAtomic), arg0, arg1)
	return &MockStateRunAtomicCall{Call: call}
}

// MockStateRunAtomicCall wrap *gomock.Call
type MockStateRunAtomicCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockStateRunAtomicCall) Return(arg0 error) *MockStateRunAtomicCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockStateRunAtomicCall) Do(f func(context.Context, func(domain.AtomicContext) error) error) *MockStateRunAtomicCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockStateRunAtomicCall) DoAndReturn(f func(context.Context, func(domain.AtomicContext) error) error) *MockStateRunAtomicCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// UpdateUnitPorts mocks base method.
func (m *MockState) UpdateUnitPorts(arg0 domain.AtomicContext, arg1 unit.UUID, arg2, arg3 network.GroupedPortRanges) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "UpdateUnitPorts", arg0, arg1, arg2, arg3)
	ret0, _ := ret[0].(error)
	return ret0
}

// UpdateUnitPorts indicates an expected call of UpdateUnitPorts.
func (mr *MockStateMockRecorder) UpdateUnitPorts(arg0, arg1, arg2, arg3 any) *MockStateUpdateUnitPortsCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "UpdateUnitPorts", reflect.TypeOf((*MockState)(nil).UpdateUnitPorts), arg0, arg1, arg2, arg3)
	return &MockStateUpdateUnitPortsCall{Call: call}
}

// MockStateUpdateUnitPortsCall wrap *gomock.Call
type MockStateUpdateUnitPortsCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockStateUpdateUnitPortsCall) Return(arg0 error) *MockStateUpdateUnitPortsCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockStateUpdateUnitPortsCall) Do(f func(domain.AtomicContext, unit.UUID, network.GroupedPortRanges, network.GroupedPortRanges) error) *MockStateUpdateUnitPortsCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockStateUpdateUnitPortsCall) DoAndReturn(f func(domain.AtomicContext, unit.UUID, network.GroupedPortRanges, network.GroupedPortRanges) error) *MockStateUpdateUnitPortsCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// WatchOpenedPortsTable mocks base method.
func (m *MockState) WatchOpenedPortsTable() string {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "WatchOpenedPortsTable")
	ret0, _ := ret[0].(string)
	return ret0
}

// WatchOpenedPortsTable indicates an expected call of WatchOpenedPortsTable.
func (mr *MockStateMockRecorder) WatchOpenedPortsTable() *MockStateWatchOpenedPortsTableCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "WatchOpenedPortsTable", reflect.TypeOf((*MockState)(nil).WatchOpenedPortsTable))
	return &MockStateWatchOpenedPortsTableCall{Call: call}
}

// MockStateWatchOpenedPortsTableCall wrap *gomock.Call
type MockStateWatchOpenedPortsTableCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockStateWatchOpenedPortsTableCall) Return(arg0 string) *MockStateWatchOpenedPortsTableCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockStateWatchOpenedPortsTableCall) Do(f func() string) *MockStateWatchOpenedPortsTableCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockStateWatchOpenedPortsTableCall) DoAndReturn(f func() string) *MockStateWatchOpenedPortsTableCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}
