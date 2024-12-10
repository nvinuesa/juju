// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/juju/juju/domain/secret/service (interfaces: SecretBackendState)
//
// Generated by this command:
//
//	mockgen -typed -package secret -destination backend_mock_test.go github.com/juju/juju/domain/secret/service SecretBackendState
//

// Package secret is a generated GoMock package.
package secret

import (
	context "context"
	reflect "reflect"

	model "github.com/juju/juju/core/model"
	secrets "github.com/juju/juju/core/secrets"
	secretbackend "github.com/juju/juju/domain/secretbackend"
	gomock "go.uber.org/mock/gomock"
)

// MockSecretBackendState is a mock of SecretBackendState interface.
type MockSecretBackendState struct {
	ctrl     *gomock.Controller
	recorder *MockSecretBackendStateMockRecorder
}

// MockSecretBackendStateMockRecorder is the mock recorder for MockSecretBackendState.
type MockSecretBackendStateMockRecorder struct {
	mock *MockSecretBackendState
}

// NewMockSecretBackendState creates a new mock instance.
func NewMockSecretBackendState(ctrl *gomock.Controller) *MockSecretBackendState {
	mock := &MockSecretBackendState{ctrl: ctrl}
	mock.recorder = &MockSecretBackendStateMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockSecretBackendState) EXPECT() *MockSecretBackendStateMockRecorder {
	return m.recorder
}

// AddSecretBackendReference mocks base method.
func (m *MockSecretBackendState) AddSecretBackendReference(arg0 context.Context, arg1 *secrets.ValueRef, arg2 model.UUID, arg3 string) (func() error, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AddSecretBackendReference", arg0, arg1, arg2, arg3)
	ret0, _ := ret[0].(func() error)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// AddSecretBackendReference indicates an expected call of AddSecretBackendReference.
func (mr *MockSecretBackendStateMockRecorder) AddSecretBackendReference(arg0, arg1, arg2, arg3 any) *MockSecretBackendStateAddSecretBackendReferenceCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AddSecretBackendReference", reflect.TypeOf((*MockSecretBackendState)(nil).AddSecretBackendReference), arg0, arg1, arg2, arg3)
	return &MockSecretBackendStateAddSecretBackendReferenceCall{Call: call}
}

// MockSecretBackendStateAddSecretBackendReferenceCall wrap *gomock.Call
type MockSecretBackendStateAddSecretBackendReferenceCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockSecretBackendStateAddSecretBackendReferenceCall) Return(arg0 func() error, arg1 error) *MockSecretBackendStateAddSecretBackendReferenceCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockSecretBackendStateAddSecretBackendReferenceCall) Do(f func(context.Context, *secrets.ValueRef, model.UUID, string) (func() error, error)) *MockSecretBackendStateAddSecretBackendReferenceCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockSecretBackendStateAddSecretBackendReferenceCall) DoAndReturn(f func(context.Context, *secrets.ValueRef, model.UUID, string) (func() error, error)) *MockSecretBackendStateAddSecretBackendReferenceCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// GetModelSecretBackendDetails mocks base method.
func (m *MockSecretBackendState) GetModelSecretBackendDetails(arg0 context.Context, arg1 model.UUID) (secretbackend.ModelSecretBackend, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetModelSecretBackendDetails", arg0, arg1)
	ret0, _ := ret[0].(secretbackend.ModelSecretBackend)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetModelSecretBackendDetails indicates an expected call of GetModelSecretBackendDetails.
func (mr *MockSecretBackendStateMockRecorder) GetModelSecretBackendDetails(arg0, arg1 any) *MockSecretBackendStateGetModelSecretBackendDetailsCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetModelSecretBackendDetails", reflect.TypeOf((*MockSecretBackendState)(nil).GetModelSecretBackendDetails), arg0, arg1)
	return &MockSecretBackendStateGetModelSecretBackendDetailsCall{Call: call}
}

// MockSecretBackendStateGetModelSecretBackendDetailsCall wrap *gomock.Call
type MockSecretBackendStateGetModelSecretBackendDetailsCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockSecretBackendStateGetModelSecretBackendDetailsCall) Return(arg0 secretbackend.ModelSecretBackend, arg1 error) *MockSecretBackendStateGetModelSecretBackendDetailsCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockSecretBackendStateGetModelSecretBackendDetailsCall) Do(f func(context.Context, model.UUID) (secretbackend.ModelSecretBackend, error)) *MockSecretBackendStateGetModelSecretBackendDetailsCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockSecretBackendStateGetModelSecretBackendDetailsCall) DoAndReturn(f func(context.Context, model.UUID) (secretbackend.ModelSecretBackend, error)) *MockSecretBackendStateGetModelSecretBackendDetailsCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// ListSecretBackendsForModel mocks base method.
func (m *MockSecretBackendState) ListSecretBackendsForModel(arg0 context.Context, arg1 model.UUID, arg2 bool) ([]*secretbackend.SecretBackend, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ListSecretBackendsForModel", arg0, arg1, arg2)
	ret0, _ := ret[0].([]*secretbackend.SecretBackend)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ListSecretBackendsForModel indicates an expected call of ListSecretBackendsForModel.
func (mr *MockSecretBackendStateMockRecorder) ListSecretBackendsForModel(arg0, arg1, arg2 any) *MockSecretBackendStateListSecretBackendsForModelCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ListSecretBackendsForModel", reflect.TypeOf((*MockSecretBackendState)(nil).ListSecretBackendsForModel), arg0, arg1, arg2)
	return &MockSecretBackendStateListSecretBackendsForModelCall{Call: call}
}

// MockSecretBackendStateListSecretBackendsForModelCall wrap *gomock.Call
type MockSecretBackendStateListSecretBackendsForModelCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockSecretBackendStateListSecretBackendsForModelCall) Return(arg0 []*secretbackend.SecretBackend, arg1 error) *MockSecretBackendStateListSecretBackendsForModelCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockSecretBackendStateListSecretBackendsForModelCall) Do(f func(context.Context, model.UUID, bool) ([]*secretbackend.SecretBackend, error)) *MockSecretBackendStateListSecretBackendsForModelCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockSecretBackendStateListSecretBackendsForModelCall) DoAndReturn(f func(context.Context, model.UUID, bool) ([]*secretbackend.SecretBackend, error)) *MockSecretBackendStateListSecretBackendsForModelCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// RemoveSecretBackendReference mocks base method.
func (m *MockSecretBackendState) RemoveSecretBackendReference(arg0 context.Context, arg1 ...string) error {
	m.ctrl.T.Helper()
	varargs := []any{arg0}
	for _, a := range arg1 {
		varargs = append(varargs, a)
	}
	ret := m.ctrl.Call(m, "RemoveSecretBackendReference", varargs...)
	ret0, _ := ret[0].(error)
	return ret0
}

// RemoveSecretBackendReference indicates an expected call of RemoveSecretBackendReference.
func (mr *MockSecretBackendStateMockRecorder) RemoveSecretBackendReference(arg0 any, arg1 ...any) *MockSecretBackendStateRemoveSecretBackendReferenceCall {
	mr.mock.ctrl.T.Helper()
	varargs := append([]any{arg0}, arg1...)
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RemoveSecretBackendReference", reflect.TypeOf((*MockSecretBackendState)(nil).RemoveSecretBackendReference), varargs...)
	return &MockSecretBackendStateRemoveSecretBackendReferenceCall{Call: call}
}

// MockSecretBackendStateRemoveSecretBackendReferenceCall wrap *gomock.Call
type MockSecretBackendStateRemoveSecretBackendReferenceCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockSecretBackendStateRemoveSecretBackendReferenceCall) Return(arg0 error) *MockSecretBackendStateRemoveSecretBackendReferenceCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockSecretBackendStateRemoveSecretBackendReferenceCall) Do(f func(context.Context, ...string) error) *MockSecretBackendStateRemoveSecretBackendReferenceCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockSecretBackendStateRemoveSecretBackendReferenceCall) DoAndReturn(f func(context.Context, ...string) error) *MockSecretBackendStateRemoveSecretBackendReferenceCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// UpdateSecretBackendReference mocks base method.
func (m *MockSecretBackendState) UpdateSecretBackendReference(arg0 context.Context, arg1 *secrets.ValueRef, arg2 model.UUID, arg3 string) (func() error, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "UpdateSecretBackendReference", arg0, arg1, arg2, arg3)
	ret0, _ := ret[0].(func() error)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// UpdateSecretBackendReference indicates an expected call of UpdateSecretBackendReference.
func (mr *MockSecretBackendStateMockRecorder) UpdateSecretBackendReference(arg0, arg1, arg2, arg3 any) *MockSecretBackendStateUpdateSecretBackendReferenceCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "UpdateSecretBackendReference", reflect.TypeOf((*MockSecretBackendState)(nil).UpdateSecretBackendReference), arg0, arg1, arg2, arg3)
	return &MockSecretBackendStateUpdateSecretBackendReferenceCall{Call: call}
}

// MockSecretBackendStateUpdateSecretBackendReferenceCall wrap *gomock.Call
type MockSecretBackendStateUpdateSecretBackendReferenceCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockSecretBackendStateUpdateSecretBackendReferenceCall) Return(arg0 func() error, arg1 error) *MockSecretBackendStateUpdateSecretBackendReferenceCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockSecretBackendStateUpdateSecretBackendReferenceCall) Do(f func(context.Context, *secrets.ValueRef, model.UUID, string) (func() error, error)) *MockSecretBackendStateUpdateSecretBackendReferenceCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockSecretBackendStateUpdateSecretBackendReferenceCall) DoAndReturn(f func(context.Context, *secrets.ValueRef, model.UUID, string) (func() error, error)) *MockSecretBackendStateUpdateSecretBackendReferenceCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}