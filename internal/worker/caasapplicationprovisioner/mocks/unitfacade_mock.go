// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/juju/juju/internal/worker/caasapplicationprovisioner (interfaces: CAASUnitProvisionerFacade)
//
// Generated by this command:
//
//	mockgen -typed -package mocks -destination mocks/unitfacade_mock.go github.com/juju/juju/internal/worker/caasapplicationprovisioner CAASUnitProvisionerFacade
//

// Package mocks is a generated GoMock package.
package mocks

import (
	reflect "reflect"

	watcher "github.com/juju/juju/core/watcher"
	params "github.com/juju/juju/rpc/params"
	gomock "go.uber.org/mock/gomock"
)

// MockCAASUnitProvisionerFacade is a mock of CAASUnitProvisionerFacade interface.
type MockCAASUnitProvisionerFacade struct {
	ctrl     *gomock.Controller
	recorder *MockCAASUnitProvisionerFacadeMockRecorder
}

// MockCAASUnitProvisionerFacadeMockRecorder is the mock recorder for MockCAASUnitProvisionerFacade.
type MockCAASUnitProvisionerFacadeMockRecorder struct {
	mock *MockCAASUnitProvisionerFacade
}

// NewMockCAASUnitProvisionerFacade creates a new mock instance.
func NewMockCAASUnitProvisionerFacade(ctrl *gomock.Controller) *MockCAASUnitProvisionerFacade {
	mock := &MockCAASUnitProvisionerFacade{ctrl: ctrl}
	mock.recorder = &MockCAASUnitProvisionerFacadeMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockCAASUnitProvisionerFacade) EXPECT() *MockCAASUnitProvisionerFacadeMockRecorder {
	return m.recorder
}

// ApplicationScale mocks base method.
func (m *MockCAASUnitProvisionerFacade) ApplicationScale(arg0 string) (int, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ApplicationScale", arg0)
	ret0, _ := ret[0].(int)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ApplicationScale indicates an expected call of ApplicationScale.
func (mr *MockCAASUnitProvisionerFacadeMockRecorder) ApplicationScale(arg0 any) *MockCAASUnitProvisionerFacadeApplicationScaleCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ApplicationScale", reflect.TypeOf((*MockCAASUnitProvisionerFacade)(nil).ApplicationScale), arg0)
	return &MockCAASUnitProvisionerFacadeApplicationScaleCall{Call: call}
}

// MockCAASUnitProvisionerFacadeApplicationScaleCall wrap *gomock.Call
type MockCAASUnitProvisionerFacadeApplicationScaleCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockCAASUnitProvisionerFacadeApplicationScaleCall) Return(arg0 int, arg1 error) *MockCAASUnitProvisionerFacadeApplicationScaleCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockCAASUnitProvisionerFacadeApplicationScaleCall) Do(f func(string) (int, error)) *MockCAASUnitProvisionerFacadeApplicationScaleCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockCAASUnitProvisionerFacadeApplicationScaleCall) DoAndReturn(f func(string) (int, error)) *MockCAASUnitProvisionerFacadeApplicationScaleCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// ApplicationTrust mocks base method.
func (m *MockCAASUnitProvisionerFacade) ApplicationTrust(arg0 string) (bool, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ApplicationTrust", arg0)
	ret0, _ := ret[0].(bool)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ApplicationTrust indicates an expected call of ApplicationTrust.
func (mr *MockCAASUnitProvisionerFacadeMockRecorder) ApplicationTrust(arg0 any) *MockCAASUnitProvisionerFacadeApplicationTrustCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ApplicationTrust", reflect.TypeOf((*MockCAASUnitProvisionerFacade)(nil).ApplicationTrust), arg0)
	return &MockCAASUnitProvisionerFacadeApplicationTrustCall{Call: call}
}

// MockCAASUnitProvisionerFacadeApplicationTrustCall wrap *gomock.Call
type MockCAASUnitProvisionerFacadeApplicationTrustCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockCAASUnitProvisionerFacadeApplicationTrustCall) Return(arg0 bool, arg1 error) *MockCAASUnitProvisionerFacadeApplicationTrustCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockCAASUnitProvisionerFacadeApplicationTrustCall) Do(f func(string) (bool, error)) *MockCAASUnitProvisionerFacadeApplicationTrustCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockCAASUnitProvisionerFacadeApplicationTrustCall) DoAndReturn(f func(string) (bool, error)) *MockCAASUnitProvisionerFacadeApplicationTrustCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// UpdateApplicationService mocks base method.
func (m *MockCAASUnitProvisionerFacade) UpdateApplicationService(arg0 params.UpdateApplicationServiceArg) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "UpdateApplicationService", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// UpdateApplicationService indicates an expected call of UpdateApplicationService.
func (mr *MockCAASUnitProvisionerFacadeMockRecorder) UpdateApplicationService(arg0 any) *MockCAASUnitProvisionerFacadeUpdateApplicationServiceCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "UpdateApplicationService", reflect.TypeOf((*MockCAASUnitProvisionerFacade)(nil).UpdateApplicationService), arg0)
	return &MockCAASUnitProvisionerFacadeUpdateApplicationServiceCall{Call: call}
}

// MockCAASUnitProvisionerFacadeUpdateApplicationServiceCall wrap *gomock.Call
type MockCAASUnitProvisionerFacadeUpdateApplicationServiceCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockCAASUnitProvisionerFacadeUpdateApplicationServiceCall) Return(arg0 error) *MockCAASUnitProvisionerFacadeUpdateApplicationServiceCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockCAASUnitProvisionerFacadeUpdateApplicationServiceCall) Do(f func(params.UpdateApplicationServiceArg) error) *MockCAASUnitProvisionerFacadeUpdateApplicationServiceCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockCAASUnitProvisionerFacadeUpdateApplicationServiceCall) DoAndReturn(f func(params.UpdateApplicationServiceArg) error) *MockCAASUnitProvisionerFacadeUpdateApplicationServiceCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// WatchApplicationScale mocks base method.
func (m *MockCAASUnitProvisionerFacade) WatchApplicationScale(arg0 string) (watcher.Watcher[struct{}], error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "WatchApplicationScale", arg0)
	ret0, _ := ret[0].(watcher.Watcher[struct{}])
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// WatchApplicationScale indicates an expected call of WatchApplicationScale.
func (mr *MockCAASUnitProvisionerFacadeMockRecorder) WatchApplicationScale(arg0 any) *MockCAASUnitProvisionerFacadeWatchApplicationScaleCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "WatchApplicationScale", reflect.TypeOf((*MockCAASUnitProvisionerFacade)(nil).WatchApplicationScale), arg0)
	return &MockCAASUnitProvisionerFacadeWatchApplicationScaleCall{Call: call}
}

// MockCAASUnitProvisionerFacadeWatchApplicationScaleCall wrap *gomock.Call
type MockCAASUnitProvisionerFacadeWatchApplicationScaleCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockCAASUnitProvisionerFacadeWatchApplicationScaleCall) Return(arg0 watcher.Watcher[struct{}], arg1 error) *MockCAASUnitProvisionerFacadeWatchApplicationScaleCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockCAASUnitProvisionerFacadeWatchApplicationScaleCall) Do(f func(string) (watcher.Watcher[struct{}], error)) *MockCAASUnitProvisionerFacadeWatchApplicationScaleCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockCAASUnitProvisionerFacadeWatchApplicationScaleCall) DoAndReturn(f func(string) (watcher.Watcher[struct{}], error)) *MockCAASUnitProvisionerFacadeWatchApplicationScaleCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// WatchApplicationTrustHash mocks base method.
func (m *MockCAASUnitProvisionerFacade) WatchApplicationTrustHash(arg0 string) (watcher.Watcher[[]string], error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "WatchApplicationTrustHash", arg0)
	ret0, _ := ret[0].(watcher.Watcher[[]string])
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// WatchApplicationTrustHash indicates an expected call of WatchApplicationTrustHash.
func (mr *MockCAASUnitProvisionerFacadeMockRecorder) WatchApplicationTrustHash(arg0 any) *MockCAASUnitProvisionerFacadeWatchApplicationTrustHashCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "WatchApplicationTrustHash", reflect.TypeOf((*MockCAASUnitProvisionerFacade)(nil).WatchApplicationTrustHash), arg0)
	return &MockCAASUnitProvisionerFacadeWatchApplicationTrustHashCall{Call: call}
}

// MockCAASUnitProvisionerFacadeWatchApplicationTrustHashCall wrap *gomock.Call
type MockCAASUnitProvisionerFacadeWatchApplicationTrustHashCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockCAASUnitProvisionerFacadeWatchApplicationTrustHashCall) Return(arg0 watcher.Watcher[[]string], arg1 error) *MockCAASUnitProvisionerFacadeWatchApplicationTrustHashCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockCAASUnitProvisionerFacadeWatchApplicationTrustHashCall) Do(f func(string) (watcher.Watcher[[]string], error)) *MockCAASUnitProvisionerFacadeWatchApplicationTrustHashCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockCAASUnitProvisionerFacadeWatchApplicationTrustHashCall) DoAndReturn(f func(string) (watcher.Watcher[[]string], error)) *MockCAASUnitProvisionerFacadeWatchApplicationTrustHashCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}
