// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/juju/juju/apiserver (interfaces: ApplicationServiceGetter,ApplicationService)
//
// Generated by this command:
//
//	mockgen -typed -package apiserver -destination application_service_mock_test.go github.com/juju/juju/apiserver ApplicationServiceGetter,ApplicationService
//

// Package apiserver is a generated GoMock package.
package apiserver

import (
	context "context"
	http "net/http"
	reflect "reflect"

	application "github.com/juju/juju/core/application"
	unit "github.com/juju/juju/core/unit"
	gomock "go.uber.org/mock/gomock"
)

// MockApplicationServiceGetter is a mock of ApplicationServiceGetter interface.
type MockApplicationServiceGetter struct {
	ctrl     *gomock.Controller
	recorder *MockApplicationServiceGetterMockRecorder
}

// MockApplicationServiceGetterMockRecorder is the mock recorder for MockApplicationServiceGetter.
type MockApplicationServiceGetterMockRecorder struct {
	mock *MockApplicationServiceGetter
}

// NewMockApplicationServiceGetter creates a new mock instance.
func NewMockApplicationServiceGetter(ctrl *gomock.Controller) *MockApplicationServiceGetter {
	mock := &MockApplicationServiceGetter{ctrl: ctrl}
	mock.recorder = &MockApplicationServiceGetterMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockApplicationServiceGetter) EXPECT() *MockApplicationServiceGetterMockRecorder {
	return m.recorder
}

// Application mocks base method.
func (m *MockApplicationServiceGetter) Application(arg0 *http.Request) (ApplicationService, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Application", arg0)
	ret0, _ := ret[0].(ApplicationService)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Application indicates an expected call of Application.
func (mr *MockApplicationServiceGetterMockRecorder) Application(arg0 any) *MockApplicationServiceGetterApplicationCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Application", reflect.TypeOf((*MockApplicationServiceGetter)(nil).Application), arg0)
	return &MockApplicationServiceGetterApplicationCall{Call: call}
}

// MockApplicationServiceGetterApplicationCall wrap *gomock.Call
type MockApplicationServiceGetterApplicationCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockApplicationServiceGetterApplicationCall) Return(arg0 ApplicationService, arg1 error) *MockApplicationServiceGetterApplicationCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockApplicationServiceGetterApplicationCall) Do(f func(*http.Request) (ApplicationService, error)) *MockApplicationServiceGetterApplicationCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockApplicationServiceGetterApplicationCall) DoAndReturn(f func(*http.Request) (ApplicationService, error)) *MockApplicationServiceGetterApplicationCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// MockApplicationService is a mock of ApplicationService interface.
type MockApplicationService struct {
	ctrl     *gomock.Controller
	recorder *MockApplicationServiceMockRecorder
}

// MockApplicationServiceMockRecorder is the mock recorder for MockApplicationService.
type MockApplicationServiceMockRecorder struct {
	mock *MockApplicationService
}

// NewMockApplicationService creates a new mock instance.
func NewMockApplicationService(ctrl *gomock.Controller) *MockApplicationService {
	mock := &MockApplicationService{ctrl: ctrl}
	mock.recorder = &MockApplicationServiceMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockApplicationService) EXPECT() *MockApplicationServiceMockRecorder {
	return m.recorder
}

// GetApplicationIDByName mocks base method.
func (m *MockApplicationService) GetApplicationIDByName(arg0 context.Context, arg1 string) (application.ID, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetApplicationIDByName", arg0, arg1)
	ret0, _ := ret[0].(application.ID)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetApplicationIDByName indicates an expected call of GetApplicationIDByName.
func (mr *MockApplicationServiceMockRecorder) GetApplicationIDByName(arg0, arg1 any) *MockApplicationServiceGetApplicationIDByNameCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetApplicationIDByName", reflect.TypeOf((*MockApplicationService)(nil).GetApplicationIDByName), arg0, arg1)
	return &MockApplicationServiceGetApplicationIDByNameCall{Call: call}
}

// MockApplicationServiceGetApplicationIDByNameCall wrap *gomock.Call
type MockApplicationServiceGetApplicationIDByNameCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockApplicationServiceGetApplicationIDByNameCall) Return(arg0 application.ID, arg1 error) *MockApplicationServiceGetApplicationIDByNameCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockApplicationServiceGetApplicationIDByNameCall) Do(f func(context.Context, string) (application.ID, error)) *MockApplicationServiceGetApplicationIDByNameCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockApplicationServiceGetApplicationIDByNameCall) DoAndReturn(f func(context.Context, string) (application.ID, error)) *MockApplicationServiceGetApplicationIDByNameCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// GetApplicationIDByUnitName mocks base method.
func (m *MockApplicationService) GetApplicationIDByUnitName(arg0 context.Context, arg1 unit.Name) (application.ID, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetApplicationIDByUnitName", arg0, arg1)
	ret0, _ := ret[0].(application.ID)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetApplicationIDByUnitName indicates an expected call of GetApplicationIDByUnitName.
func (mr *MockApplicationServiceMockRecorder) GetApplicationIDByUnitName(arg0, arg1 any) *MockApplicationServiceGetApplicationIDByUnitNameCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetApplicationIDByUnitName", reflect.TypeOf((*MockApplicationService)(nil).GetApplicationIDByUnitName), arg0, arg1)
	return &MockApplicationServiceGetApplicationIDByUnitNameCall{Call: call}
}

// MockApplicationServiceGetApplicationIDByUnitNameCall wrap *gomock.Call
type MockApplicationServiceGetApplicationIDByUnitNameCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockApplicationServiceGetApplicationIDByUnitNameCall) Return(arg0 application.ID, arg1 error) *MockApplicationServiceGetApplicationIDByUnitNameCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockApplicationServiceGetApplicationIDByUnitNameCall) Do(f func(context.Context, unit.Name) (application.ID, error)) *MockApplicationServiceGetApplicationIDByUnitNameCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockApplicationServiceGetApplicationIDByUnitNameCall) DoAndReturn(f func(context.Context, unit.Name) (application.ID, error)) *MockApplicationServiceGetApplicationIDByUnitNameCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// GetUnitUUID mocks base method.
func (m *MockApplicationService) GetUnitUUID(arg0 context.Context, arg1 unit.Name) (unit.UUID, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetUnitUUID", arg0, arg1)
	ret0, _ := ret[0].(unit.UUID)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetUnitUUID indicates an expected call of GetUnitUUID.
func (mr *MockApplicationServiceMockRecorder) GetUnitUUID(arg0, arg1 any) *MockApplicationServiceGetUnitUUIDCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetUnitUUID", reflect.TypeOf((*MockApplicationService)(nil).GetUnitUUID), arg0, arg1)
	return &MockApplicationServiceGetUnitUUIDCall{Call: call}
}

// MockApplicationServiceGetUnitUUIDCall wrap *gomock.Call
type MockApplicationServiceGetUnitUUIDCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockApplicationServiceGetUnitUUIDCall) Return(arg0 unit.UUID, arg1 error) *MockApplicationServiceGetUnitUUIDCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockApplicationServiceGetUnitUUIDCall) Do(f func(context.Context, unit.Name) (unit.UUID, error)) *MockApplicationServiceGetUnitUUIDCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockApplicationServiceGetUnitUUIDCall) DoAndReturn(f func(context.Context, unit.Name) (unit.UUID, error)) *MockApplicationServiceGetUnitUUIDCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}
