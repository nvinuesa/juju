// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/juju/juju/apiserver/facades/client/client (interfaces: BlockDeviceService,NetworkService)
//
// Generated by this command:
//
//	mockgen -package client_test -destination service_mock_test.go github.com/juju/juju/apiserver/facades/client/client BlockDeviceService,NetworkService
//

// Package client_test is a generated GoMock package.
package client_test

import (
	context "context"
	reflect "reflect"

	blockdevice "github.com/juju/juju/core/blockdevice"
	network "github.com/juju/juju/core/network"
	gomock "go.uber.org/mock/gomock"
)

// MockBlockDeviceService is a mock of BlockDeviceService interface.
type MockBlockDeviceService struct {
	ctrl     *gomock.Controller
	recorder *MockBlockDeviceServiceMockRecorder
}

// MockBlockDeviceServiceMockRecorder is the mock recorder for MockBlockDeviceService.
type MockBlockDeviceServiceMockRecorder struct {
	mock *MockBlockDeviceService
}

// NewMockBlockDeviceService creates a new mock instance.
func NewMockBlockDeviceService(ctrl *gomock.Controller) *MockBlockDeviceService {
	mock := &MockBlockDeviceService{ctrl: ctrl}
	mock.recorder = &MockBlockDeviceServiceMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockBlockDeviceService) EXPECT() *MockBlockDeviceServiceMockRecorder {
	return m.recorder
}

// BlockDevices mocks base method.
func (m *MockBlockDeviceService) BlockDevices(arg0 context.Context, arg1 string) ([]blockdevice.BlockDevice, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "BlockDevices", arg0, arg1)
	ret0, _ := ret[0].([]blockdevice.BlockDevice)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// BlockDevices indicates an expected call of BlockDevices.
func (mr *MockBlockDeviceServiceMockRecorder) BlockDevices(arg0, arg1 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "BlockDevices", reflect.TypeOf((*MockBlockDeviceService)(nil).BlockDevices), arg0, arg1)
}

// MockNetworkService is a mock of NetworkService interface.
type MockNetworkService struct {
	ctrl     *gomock.Controller
	recorder *MockNetworkServiceMockRecorder
}

// MockNetworkServiceMockRecorder is the mock recorder for MockNetworkService.
type MockNetworkServiceMockRecorder struct {
	mock *MockNetworkService
}

// NewMockNetworkService creates a new mock instance.
func NewMockNetworkService(ctrl *gomock.Controller) *MockNetworkService {
	mock := &MockNetworkService{ctrl: ctrl}
	mock.recorder = &MockNetworkServiceMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockNetworkService) EXPECT() *MockNetworkServiceMockRecorder {
	return m.recorder
}

// GetAllSpaces mocks base method.
func (m *MockNetworkService) GetAllSpaces(arg0 context.Context) (network.SpaceInfos, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetAllSpaces", arg0)
	ret0, _ := ret[0].(network.SpaceInfos)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetAllSpaces indicates an expected call of GetAllSpaces.
func (mr *MockNetworkServiceMockRecorder) GetAllSpaces(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetAllSpaces", reflect.TypeOf((*MockNetworkService)(nil).GetAllSpaces), arg0)
}

// GetAllSubnets mocks base method.
func (m *MockNetworkService) GetAllSubnets(arg0 context.Context) (network.SubnetInfos, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetAllSubnets", arg0)
	ret0, _ := ret[0].(network.SubnetInfos)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetAllSubnets indicates an expected call of GetAllSubnets.
func (mr *MockNetworkServiceMockRecorder) GetAllSubnets(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetAllSubnets", reflect.TypeOf((*MockNetworkService)(nil).GetAllSubnets), arg0)
}