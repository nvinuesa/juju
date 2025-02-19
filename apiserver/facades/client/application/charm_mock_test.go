// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/juju/juju/internal/charm (interfaces: Charm,CharmMeta)
//
// Generated by this command:
//
//	mockgen -typed -package application -destination charm_mock_test.go github.com/juju/juju/internal/charm Charm,CharmMeta
//

// Package application is a generated GoMock package.
package application

import (
	reflect "reflect"

	charm "github.com/juju/juju/internal/charm"
	gomock "go.uber.org/mock/gomock"
)

// MockCharm is a mock of Charm interface.
type MockCharm struct {
	ctrl     *gomock.Controller
	recorder *MockCharmMockRecorder
}

// MockCharmMockRecorder is the mock recorder for MockCharm.
type MockCharmMockRecorder struct {
	mock *MockCharm
}

// NewMockCharm creates a new mock instance.
func NewMockCharm(ctrl *gomock.Controller) *MockCharm {
	mock := &MockCharm{ctrl: ctrl}
	mock.recorder = &MockCharmMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockCharm) EXPECT() *MockCharmMockRecorder {
	return m.recorder
}

// Actions mocks base method.
func (m *MockCharm) Actions() *charm.Actions {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Actions")
	ret0, _ := ret[0].(*charm.Actions)
	return ret0
}

// Actions indicates an expected call of Actions.
func (mr *MockCharmMockRecorder) Actions() *MockCharmActionsCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Actions", reflect.TypeOf((*MockCharm)(nil).Actions))
	return &MockCharmActionsCall{Call: call}
}

// MockCharmActionsCall wrap *gomock.Call
type MockCharmActionsCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockCharmActionsCall) Return(arg0 *charm.Actions) *MockCharmActionsCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockCharmActionsCall) Do(f func() *charm.Actions) *MockCharmActionsCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockCharmActionsCall) DoAndReturn(f func() *charm.Actions) *MockCharmActionsCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// Config mocks base method.
func (m *MockCharm) Config() *charm.Config {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Config")
	ret0, _ := ret[0].(*charm.Config)
	return ret0
}

// Config indicates an expected call of Config.
func (mr *MockCharmMockRecorder) Config() *MockCharmConfigCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Config", reflect.TypeOf((*MockCharm)(nil).Config))
	return &MockCharmConfigCall{Call: call}
}

// MockCharmConfigCall wrap *gomock.Call
type MockCharmConfigCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockCharmConfigCall) Return(arg0 *charm.Config) *MockCharmConfigCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockCharmConfigCall) Do(f func() *charm.Config) *MockCharmConfigCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockCharmConfigCall) DoAndReturn(f func() *charm.Config) *MockCharmConfigCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// Manifest mocks base method.
func (m *MockCharm) Manifest() *charm.Manifest {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Manifest")
	ret0, _ := ret[0].(*charm.Manifest)
	return ret0
}

// Manifest indicates an expected call of Manifest.
func (mr *MockCharmMockRecorder) Manifest() *MockCharmManifestCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Manifest", reflect.TypeOf((*MockCharm)(nil).Manifest))
	return &MockCharmManifestCall{Call: call}
}

// MockCharmManifestCall wrap *gomock.Call
type MockCharmManifestCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockCharmManifestCall) Return(arg0 *charm.Manifest) *MockCharmManifestCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockCharmManifestCall) Do(f func() *charm.Manifest) *MockCharmManifestCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockCharmManifestCall) DoAndReturn(f func() *charm.Manifest) *MockCharmManifestCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// Meta mocks base method.
func (m *MockCharm) Meta() *charm.Meta {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Meta")
	ret0, _ := ret[0].(*charm.Meta)
	return ret0
}

// Meta indicates an expected call of Meta.
func (mr *MockCharmMockRecorder) Meta() *MockCharmMetaCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Meta", reflect.TypeOf((*MockCharm)(nil).Meta))
	return &MockCharmMetaCall{Call: call}
}

// MockCharmMetaCall wrap *gomock.Call
type MockCharmMetaCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockCharmMetaCall) Return(arg0 *charm.Meta) *MockCharmMetaCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockCharmMetaCall) Do(f func() *charm.Meta) *MockCharmMetaCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockCharmMetaCall) DoAndReturn(f func() *charm.Meta) *MockCharmMetaCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// Revision mocks base method.
func (m *MockCharm) Revision() int {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Revision")
	ret0, _ := ret[0].(int)
	return ret0
}

// Revision indicates an expected call of Revision.
func (mr *MockCharmMockRecorder) Revision() *MockCharmRevisionCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Revision", reflect.TypeOf((*MockCharm)(nil).Revision))
	return &MockCharmRevisionCall{Call: call}
}

// MockCharmRevisionCall wrap *gomock.Call
type MockCharmRevisionCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockCharmRevisionCall) Return(arg0 int) *MockCharmRevisionCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockCharmRevisionCall) Do(f func() int) *MockCharmRevisionCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockCharmRevisionCall) DoAndReturn(f func() int) *MockCharmRevisionCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// Version mocks base method.
func (m *MockCharm) Version() string {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Version")
	ret0, _ := ret[0].(string)
	return ret0
}

// Version indicates an expected call of Version.
func (mr *MockCharmMockRecorder) Version() *MockCharmVersionCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Version", reflect.TypeOf((*MockCharm)(nil).Version))
	return &MockCharmVersionCall{Call: call}
}

// MockCharmVersionCall wrap *gomock.Call
type MockCharmVersionCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockCharmVersionCall) Return(arg0 string) *MockCharmVersionCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockCharmVersionCall) Do(f func() string) *MockCharmVersionCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockCharmVersionCall) DoAndReturn(f func() string) *MockCharmVersionCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// MockCharmMeta is a mock of CharmMeta interface.
type MockCharmMeta struct {
	ctrl     *gomock.Controller
	recorder *MockCharmMetaMockRecorder
}

// MockCharmMetaMockRecorder is the mock recorder for MockCharmMeta.
type MockCharmMetaMockRecorder struct {
	mock *MockCharmMeta
}

// NewMockCharmMeta creates a new mock instance.
func NewMockCharmMeta(ctrl *gomock.Controller) *MockCharmMeta {
	mock := &MockCharmMeta{ctrl: ctrl}
	mock.recorder = &MockCharmMetaMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockCharmMeta) EXPECT() *MockCharmMetaMockRecorder {
	return m.recorder
}

// Manifest mocks base method.
func (m *MockCharmMeta) Manifest() *charm.Manifest {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Manifest")
	ret0, _ := ret[0].(*charm.Manifest)
	return ret0
}

// Manifest indicates an expected call of Manifest.
func (mr *MockCharmMetaMockRecorder) Manifest() *MockCharmMetaManifestCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Manifest", reflect.TypeOf((*MockCharmMeta)(nil).Manifest))
	return &MockCharmMetaManifestCall{Call: call}
}

// MockCharmMetaManifestCall wrap *gomock.Call
type MockCharmMetaManifestCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockCharmMetaManifestCall) Return(arg0 *charm.Manifest) *MockCharmMetaManifestCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockCharmMetaManifestCall) Do(f func() *charm.Manifest) *MockCharmMetaManifestCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockCharmMetaManifestCall) DoAndReturn(f func() *charm.Manifest) *MockCharmMetaManifestCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// Meta mocks base method.
func (m *MockCharmMeta) Meta() *charm.Meta {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Meta")
	ret0, _ := ret[0].(*charm.Meta)
	return ret0
}

// Meta indicates an expected call of Meta.
func (mr *MockCharmMetaMockRecorder) Meta() *MockCharmMetaMetaCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Meta", reflect.TypeOf((*MockCharmMeta)(nil).Meta))
	return &MockCharmMetaMetaCall{Call: call}
}

// MockCharmMetaMetaCall wrap *gomock.Call
type MockCharmMetaMetaCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockCharmMetaMetaCall) Return(arg0 *charm.Meta) *MockCharmMetaMetaCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockCharmMetaMetaCall) Do(f func() *charm.Meta) *MockCharmMetaMetaCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockCharmMetaMetaCall) DoAndReturn(f func() *charm.Meta) *MockCharmMetaMetaCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}
