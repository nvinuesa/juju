// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/juju/juju/cmd/juju/secrets (interfaces: ListSecretsAPI,AddSecretsAPI,GrantRevokeSecretsAPI,UpdateSecretsAPI,RemoveSecretsAPI)
//
// Generated by this command:
//
//	mockgen -typed -package mocks -destination mocks/secretsapi.go github.com/juju/juju/cmd/juju/secrets ListSecretsAPI,AddSecretsAPI,GrantRevokeSecretsAPI,UpdateSecretsAPI,RemoveSecretsAPI
//

// Package mocks is a generated GoMock package.
package mocks

import (
	context "context"
	reflect "reflect"

	secrets "github.com/juju/juju/api/client/secrets"
	secrets0 "github.com/juju/juju/core/secrets"
	gomock "go.uber.org/mock/gomock"
)

// MockListSecretsAPI is a mock of ListSecretsAPI interface.
type MockListSecretsAPI struct {
	ctrl     *gomock.Controller
	recorder *MockListSecretsAPIMockRecorder
}

// MockListSecretsAPIMockRecorder is the mock recorder for MockListSecretsAPI.
type MockListSecretsAPIMockRecorder struct {
	mock *MockListSecretsAPI
}

// NewMockListSecretsAPI creates a new mock instance.
func NewMockListSecretsAPI(ctrl *gomock.Controller) *MockListSecretsAPI {
	mock := &MockListSecretsAPI{ctrl: ctrl}
	mock.recorder = &MockListSecretsAPIMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockListSecretsAPI) EXPECT() *MockListSecretsAPIMockRecorder {
	return m.recorder
}

// Close mocks base method.
func (m *MockListSecretsAPI) Close() error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Close")
	ret0, _ := ret[0].(error)
	return ret0
}

// Close indicates an expected call of Close.
func (mr *MockListSecretsAPIMockRecorder) Close() *MockListSecretsAPICloseCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Close", reflect.TypeOf((*MockListSecretsAPI)(nil).Close))
	return &MockListSecretsAPICloseCall{Call: call}
}

// MockListSecretsAPICloseCall wrap *gomock.Call
type MockListSecretsAPICloseCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockListSecretsAPICloseCall) Return(arg0 error) *MockListSecretsAPICloseCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockListSecretsAPICloseCall) Do(f func() error) *MockListSecretsAPICloseCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockListSecretsAPICloseCall) DoAndReturn(f func() error) *MockListSecretsAPICloseCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// ListSecrets mocks base method.
func (m *MockListSecretsAPI) ListSecrets(arg0 context.Context, arg1 bool, arg2 secrets0.Filter) ([]secrets.SecretDetails, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ListSecrets", arg0, arg1, arg2)
	ret0, _ := ret[0].([]secrets.SecretDetails)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ListSecrets indicates an expected call of ListSecrets.
func (mr *MockListSecretsAPIMockRecorder) ListSecrets(arg0, arg1, arg2 any) *MockListSecretsAPIListSecretsCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ListSecrets", reflect.TypeOf((*MockListSecretsAPI)(nil).ListSecrets), arg0, arg1, arg2)
	return &MockListSecretsAPIListSecretsCall{Call: call}
}

// MockListSecretsAPIListSecretsCall wrap *gomock.Call
type MockListSecretsAPIListSecretsCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockListSecretsAPIListSecretsCall) Return(arg0 []secrets.SecretDetails, arg1 error) *MockListSecretsAPIListSecretsCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockListSecretsAPIListSecretsCall) Do(f func(context.Context, bool, secrets0.Filter) ([]secrets.SecretDetails, error)) *MockListSecretsAPIListSecretsCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockListSecretsAPIListSecretsCall) DoAndReturn(f func(context.Context, bool, secrets0.Filter) ([]secrets.SecretDetails, error)) *MockListSecretsAPIListSecretsCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// MockAddSecretsAPI is a mock of AddSecretsAPI interface.
type MockAddSecretsAPI struct {
	ctrl     *gomock.Controller
	recorder *MockAddSecretsAPIMockRecorder
}

// MockAddSecretsAPIMockRecorder is the mock recorder for MockAddSecretsAPI.
type MockAddSecretsAPIMockRecorder struct {
	mock *MockAddSecretsAPI
}

// NewMockAddSecretsAPI creates a new mock instance.
func NewMockAddSecretsAPI(ctrl *gomock.Controller) *MockAddSecretsAPI {
	mock := &MockAddSecretsAPI{ctrl: ctrl}
	mock.recorder = &MockAddSecretsAPIMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockAddSecretsAPI) EXPECT() *MockAddSecretsAPIMockRecorder {
	return m.recorder
}

// Close mocks base method.
func (m *MockAddSecretsAPI) Close() error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Close")
	ret0, _ := ret[0].(error)
	return ret0
}

// Close indicates an expected call of Close.
func (mr *MockAddSecretsAPIMockRecorder) Close() *MockAddSecretsAPICloseCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Close", reflect.TypeOf((*MockAddSecretsAPI)(nil).Close))
	return &MockAddSecretsAPICloseCall{Call: call}
}

// MockAddSecretsAPICloseCall wrap *gomock.Call
type MockAddSecretsAPICloseCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockAddSecretsAPICloseCall) Return(arg0 error) *MockAddSecretsAPICloseCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockAddSecretsAPICloseCall) Do(f func() error) *MockAddSecretsAPICloseCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockAddSecretsAPICloseCall) DoAndReturn(f func() error) *MockAddSecretsAPICloseCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// CreateSecret mocks base method.
func (m *MockAddSecretsAPI) CreateSecret(arg0 context.Context, arg1, arg2 string, arg3 map[string]string) (string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CreateSecret", arg0, arg1, arg2, arg3)
	ret0, _ := ret[0].(string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// CreateSecret indicates an expected call of CreateSecret.
func (mr *MockAddSecretsAPIMockRecorder) CreateSecret(arg0, arg1, arg2, arg3 any) *MockAddSecretsAPICreateSecretCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CreateSecret", reflect.TypeOf((*MockAddSecretsAPI)(nil).CreateSecret), arg0, arg1, arg2, arg3)
	return &MockAddSecretsAPICreateSecretCall{Call: call}
}

// MockAddSecretsAPICreateSecretCall wrap *gomock.Call
type MockAddSecretsAPICreateSecretCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockAddSecretsAPICreateSecretCall) Return(arg0 string, arg1 error) *MockAddSecretsAPICreateSecretCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockAddSecretsAPICreateSecretCall) Do(f func(context.Context, string, string, map[string]string) (string, error)) *MockAddSecretsAPICreateSecretCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockAddSecretsAPICreateSecretCall) DoAndReturn(f func(context.Context, string, string, map[string]string) (string, error)) *MockAddSecretsAPICreateSecretCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// MockGrantRevokeSecretsAPI is a mock of GrantRevokeSecretsAPI interface.
type MockGrantRevokeSecretsAPI struct {
	ctrl     *gomock.Controller
	recorder *MockGrantRevokeSecretsAPIMockRecorder
}

// MockGrantRevokeSecretsAPIMockRecorder is the mock recorder for MockGrantRevokeSecretsAPI.
type MockGrantRevokeSecretsAPIMockRecorder struct {
	mock *MockGrantRevokeSecretsAPI
}

// NewMockGrantRevokeSecretsAPI creates a new mock instance.
func NewMockGrantRevokeSecretsAPI(ctrl *gomock.Controller) *MockGrantRevokeSecretsAPI {
	mock := &MockGrantRevokeSecretsAPI{ctrl: ctrl}
	mock.recorder = &MockGrantRevokeSecretsAPIMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockGrantRevokeSecretsAPI) EXPECT() *MockGrantRevokeSecretsAPIMockRecorder {
	return m.recorder
}

// Close mocks base method.
func (m *MockGrantRevokeSecretsAPI) Close() error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Close")
	ret0, _ := ret[0].(error)
	return ret0
}

// Close indicates an expected call of Close.
func (mr *MockGrantRevokeSecretsAPIMockRecorder) Close() *MockGrantRevokeSecretsAPICloseCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Close", reflect.TypeOf((*MockGrantRevokeSecretsAPI)(nil).Close))
	return &MockGrantRevokeSecretsAPICloseCall{Call: call}
}

// MockGrantRevokeSecretsAPICloseCall wrap *gomock.Call
type MockGrantRevokeSecretsAPICloseCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockGrantRevokeSecretsAPICloseCall) Return(arg0 error) *MockGrantRevokeSecretsAPICloseCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockGrantRevokeSecretsAPICloseCall) Do(f func() error) *MockGrantRevokeSecretsAPICloseCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockGrantRevokeSecretsAPICloseCall) DoAndReturn(f func() error) *MockGrantRevokeSecretsAPICloseCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// GrantSecret mocks base method.
func (m *MockGrantRevokeSecretsAPI) GrantSecret(arg0 context.Context, arg1 *secrets0.URI, arg2 string, arg3 []string) ([]error, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GrantSecret", arg0, arg1, arg2, arg3)
	ret0, _ := ret[0].([]error)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GrantSecret indicates an expected call of GrantSecret.
func (mr *MockGrantRevokeSecretsAPIMockRecorder) GrantSecret(arg0, arg1, arg2, arg3 any) *MockGrantRevokeSecretsAPIGrantSecretCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GrantSecret", reflect.TypeOf((*MockGrantRevokeSecretsAPI)(nil).GrantSecret), arg0, arg1, arg2, arg3)
	return &MockGrantRevokeSecretsAPIGrantSecretCall{Call: call}
}

// MockGrantRevokeSecretsAPIGrantSecretCall wrap *gomock.Call
type MockGrantRevokeSecretsAPIGrantSecretCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockGrantRevokeSecretsAPIGrantSecretCall) Return(arg0 []error, arg1 error) *MockGrantRevokeSecretsAPIGrantSecretCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockGrantRevokeSecretsAPIGrantSecretCall) Do(f func(context.Context, *secrets0.URI, string, []string) ([]error, error)) *MockGrantRevokeSecretsAPIGrantSecretCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockGrantRevokeSecretsAPIGrantSecretCall) DoAndReturn(f func(context.Context, *secrets0.URI, string, []string) ([]error, error)) *MockGrantRevokeSecretsAPIGrantSecretCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// RevokeSecret mocks base method.
func (m *MockGrantRevokeSecretsAPI) RevokeSecret(arg0 context.Context, arg1 *secrets0.URI, arg2 string, arg3 []string) ([]error, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "RevokeSecret", arg0, arg1, arg2, arg3)
	ret0, _ := ret[0].([]error)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// RevokeSecret indicates an expected call of RevokeSecret.
func (mr *MockGrantRevokeSecretsAPIMockRecorder) RevokeSecret(arg0, arg1, arg2, arg3 any) *MockGrantRevokeSecretsAPIRevokeSecretCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RevokeSecret", reflect.TypeOf((*MockGrantRevokeSecretsAPI)(nil).RevokeSecret), arg0, arg1, arg2, arg3)
	return &MockGrantRevokeSecretsAPIRevokeSecretCall{Call: call}
}

// MockGrantRevokeSecretsAPIRevokeSecretCall wrap *gomock.Call
type MockGrantRevokeSecretsAPIRevokeSecretCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockGrantRevokeSecretsAPIRevokeSecretCall) Return(arg0 []error, arg1 error) *MockGrantRevokeSecretsAPIRevokeSecretCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockGrantRevokeSecretsAPIRevokeSecretCall) Do(f func(context.Context, *secrets0.URI, string, []string) ([]error, error)) *MockGrantRevokeSecretsAPIRevokeSecretCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockGrantRevokeSecretsAPIRevokeSecretCall) DoAndReturn(f func(context.Context, *secrets0.URI, string, []string) ([]error, error)) *MockGrantRevokeSecretsAPIRevokeSecretCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// MockUpdateSecretsAPI is a mock of UpdateSecretsAPI interface.
type MockUpdateSecretsAPI struct {
	ctrl     *gomock.Controller
	recorder *MockUpdateSecretsAPIMockRecorder
}

// MockUpdateSecretsAPIMockRecorder is the mock recorder for MockUpdateSecretsAPI.
type MockUpdateSecretsAPIMockRecorder struct {
	mock *MockUpdateSecretsAPI
}

// NewMockUpdateSecretsAPI creates a new mock instance.
func NewMockUpdateSecretsAPI(ctrl *gomock.Controller) *MockUpdateSecretsAPI {
	mock := &MockUpdateSecretsAPI{ctrl: ctrl}
	mock.recorder = &MockUpdateSecretsAPIMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockUpdateSecretsAPI) EXPECT() *MockUpdateSecretsAPIMockRecorder {
	return m.recorder
}

// Close mocks base method.
func (m *MockUpdateSecretsAPI) Close() error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Close")
	ret0, _ := ret[0].(error)
	return ret0
}

// Close indicates an expected call of Close.
func (mr *MockUpdateSecretsAPIMockRecorder) Close() *MockUpdateSecretsAPICloseCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Close", reflect.TypeOf((*MockUpdateSecretsAPI)(nil).Close))
	return &MockUpdateSecretsAPICloseCall{Call: call}
}

// MockUpdateSecretsAPICloseCall wrap *gomock.Call
type MockUpdateSecretsAPICloseCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockUpdateSecretsAPICloseCall) Return(arg0 error) *MockUpdateSecretsAPICloseCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockUpdateSecretsAPICloseCall) Do(f func() error) *MockUpdateSecretsAPICloseCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockUpdateSecretsAPICloseCall) DoAndReturn(f func() error) *MockUpdateSecretsAPICloseCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// UpdateSecret mocks base method.
func (m *MockUpdateSecretsAPI) UpdateSecret(arg0 context.Context, arg1 *secrets0.URI, arg2 string, arg3 *bool, arg4, arg5 string, arg6 map[string]string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "UpdateSecret", arg0, arg1, arg2, arg3, arg4, arg5, arg6)
	ret0, _ := ret[0].(error)
	return ret0
}

// UpdateSecret indicates an expected call of UpdateSecret.
func (mr *MockUpdateSecretsAPIMockRecorder) UpdateSecret(arg0, arg1, arg2, arg3, arg4, arg5, arg6 any) *MockUpdateSecretsAPIUpdateSecretCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "UpdateSecret", reflect.TypeOf((*MockUpdateSecretsAPI)(nil).UpdateSecret), arg0, arg1, arg2, arg3, arg4, arg5, arg6)
	return &MockUpdateSecretsAPIUpdateSecretCall{Call: call}
}

// MockUpdateSecretsAPIUpdateSecretCall wrap *gomock.Call
type MockUpdateSecretsAPIUpdateSecretCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockUpdateSecretsAPIUpdateSecretCall) Return(arg0 error) *MockUpdateSecretsAPIUpdateSecretCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockUpdateSecretsAPIUpdateSecretCall) Do(f func(context.Context, *secrets0.URI, string, *bool, string, string, map[string]string) error) *MockUpdateSecretsAPIUpdateSecretCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockUpdateSecretsAPIUpdateSecretCall) DoAndReturn(f func(context.Context, *secrets0.URI, string, *bool, string, string, map[string]string) error) *MockUpdateSecretsAPIUpdateSecretCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// MockRemoveSecretsAPI is a mock of RemoveSecretsAPI interface.
type MockRemoveSecretsAPI struct {
	ctrl     *gomock.Controller
	recorder *MockRemoveSecretsAPIMockRecorder
}

// MockRemoveSecretsAPIMockRecorder is the mock recorder for MockRemoveSecretsAPI.
type MockRemoveSecretsAPIMockRecorder struct {
	mock *MockRemoveSecretsAPI
}

// NewMockRemoveSecretsAPI creates a new mock instance.
func NewMockRemoveSecretsAPI(ctrl *gomock.Controller) *MockRemoveSecretsAPI {
	mock := &MockRemoveSecretsAPI{ctrl: ctrl}
	mock.recorder = &MockRemoveSecretsAPIMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockRemoveSecretsAPI) EXPECT() *MockRemoveSecretsAPIMockRecorder {
	return m.recorder
}

// Close mocks base method.
func (m *MockRemoveSecretsAPI) Close() error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Close")
	ret0, _ := ret[0].(error)
	return ret0
}

// Close indicates an expected call of Close.
func (mr *MockRemoveSecretsAPIMockRecorder) Close() *MockRemoveSecretsAPICloseCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Close", reflect.TypeOf((*MockRemoveSecretsAPI)(nil).Close))
	return &MockRemoveSecretsAPICloseCall{Call: call}
}

// MockRemoveSecretsAPICloseCall wrap *gomock.Call
type MockRemoveSecretsAPICloseCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockRemoveSecretsAPICloseCall) Return(arg0 error) *MockRemoveSecretsAPICloseCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockRemoveSecretsAPICloseCall) Do(f func() error) *MockRemoveSecretsAPICloseCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockRemoveSecretsAPICloseCall) DoAndReturn(f func() error) *MockRemoveSecretsAPICloseCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// RemoveSecret mocks base method.
func (m *MockRemoveSecretsAPI) RemoveSecret(arg0 context.Context, arg1 *secrets0.URI, arg2 string, arg3 *int) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "RemoveSecret", arg0, arg1, arg2, arg3)
	ret0, _ := ret[0].(error)
	return ret0
}

// RemoveSecret indicates an expected call of RemoveSecret.
func (mr *MockRemoveSecretsAPIMockRecorder) RemoveSecret(arg0, arg1, arg2, arg3 any) *MockRemoveSecretsAPIRemoveSecretCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RemoveSecret", reflect.TypeOf((*MockRemoveSecretsAPI)(nil).RemoveSecret), arg0, arg1, arg2, arg3)
	return &MockRemoveSecretsAPIRemoveSecretCall{Call: call}
}

// MockRemoveSecretsAPIRemoveSecretCall wrap *gomock.Call
type MockRemoveSecretsAPIRemoveSecretCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockRemoveSecretsAPIRemoveSecretCall) Return(arg0 error) *MockRemoveSecretsAPIRemoveSecretCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockRemoveSecretsAPIRemoveSecretCall) Do(f func(context.Context, *secrets0.URI, string, *int) error) *MockRemoveSecretsAPIRemoveSecretCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockRemoveSecretsAPIRemoveSecretCall) DoAndReturn(f func(context.Context, *secrets0.URI, string, *int) error) *MockRemoveSecretsAPIRemoveSecretCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}
