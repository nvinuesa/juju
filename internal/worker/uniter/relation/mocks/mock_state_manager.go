// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/juju/juju/internal/worker/uniter/relation (interfaces: StateManager)
//
// Generated by this command:
//
//	mockgen -typed -package mocks -destination mocks/mock_state_manager.go github.com/juju/juju/internal/worker/uniter/relation StateManager
//

// Package mocks is a generated GoMock package.
package mocks

import (
	context "context"
	reflect "reflect"

	relation "github.com/juju/juju/internal/worker/uniter/relation"
	gomock "go.uber.org/mock/gomock"
)

// MockStateManager is a mock of StateManager interface.
type MockStateManager struct {
	ctrl     *gomock.Controller
	recorder *MockStateManagerMockRecorder
}

// MockStateManagerMockRecorder is the mock recorder for MockStateManager.
type MockStateManagerMockRecorder struct {
	mock *MockStateManager
}

// NewMockStateManager creates a new mock instance.
func NewMockStateManager(ctrl *gomock.Controller) *MockStateManager {
	mock := &MockStateManager{ctrl: ctrl}
	mock.recorder = &MockStateManagerMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockStateManager) EXPECT() *MockStateManagerMockRecorder {
	return m.recorder
}

// KnownIDs mocks base method.
func (m *MockStateManager) KnownIDs() []int {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "KnownIDs")
	ret0, _ := ret[0].([]int)
	return ret0
}

// KnownIDs indicates an expected call of KnownIDs.
func (mr *MockStateManagerMockRecorder) KnownIDs() *MockStateManagerKnownIDsCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "KnownIDs", reflect.TypeOf((*MockStateManager)(nil).KnownIDs))
	return &MockStateManagerKnownIDsCall{Call: call}
}

// MockStateManagerKnownIDsCall wrap *gomock.Call
type MockStateManagerKnownIDsCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockStateManagerKnownIDsCall) Return(arg0 []int) *MockStateManagerKnownIDsCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockStateManagerKnownIDsCall) Do(f func() []int) *MockStateManagerKnownIDsCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockStateManagerKnownIDsCall) DoAndReturn(f func() []int) *MockStateManagerKnownIDsCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// Relation mocks base method.
func (m *MockStateManager) Relation(arg0 int) (*relation.State, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Relation", arg0)
	ret0, _ := ret[0].(*relation.State)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Relation indicates an expected call of Relation.
func (mr *MockStateManagerMockRecorder) Relation(arg0 any) *MockStateManagerRelationCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Relation", reflect.TypeOf((*MockStateManager)(nil).Relation), arg0)
	return &MockStateManagerRelationCall{Call: call}
}

// MockStateManagerRelationCall wrap *gomock.Call
type MockStateManagerRelationCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockStateManagerRelationCall) Return(arg0 *relation.State, arg1 error) *MockStateManagerRelationCall {
	c.Call = c.Call.Return(arg0, arg1)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockStateManagerRelationCall) Do(f func(int) (*relation.State, error)) *MockStateManagerRelationCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockStateManagerRelationCall) DoAndReturn(f func(int) (*relation.State, error)) *MockStateManagerRelationCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// RelationFound mocks base method.
func (m *MockStateManager) RelationFound(arg0 int) bool {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "RelationFound", arg0)
	ret0, _ := ret[0].(bool)
	return ret0
}

// RelationFound indicates an expected call of RelationFound.
func (mr *MockStateManagerMockRecorder) RelationFound(arg0 any) *MockStateManagerRelationFoundCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RelationFound", reflect.TypeOf((*MockStateManager)(nil).RelationFound), arg0)
	return &MockStateManagerRelationFoundCall{Call: call}
}

// MockStateManagerRelationFoundCall wrap *gomock.Call
type MockStateManagerRelationFoundCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockStateManagerRelationFoundCall) Return(arg0 bool) *MockStateManagerRelationFoundCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockStateManagerRelationFoundCall) Do(f func(int) bool) *MockStateManagerRelationFoundCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockStateManagerRelationFoundCall) DoAndReturn(f func(int) bool) *MockStateManagerRelationFoundCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// RemoveRelation mocks base method.
func (m *MockStateManager) RemoveRelation(arg0 context.Context, arg1 int, arg2 relation.UnitGetter, arg3 map[string]bool) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "RemoveRelation", arg0, arg1, arg2, arg3)
	ret0, _ := ret[0].(error)
	return ret0
}

// RemoveRelation indicates an expected call of RemoveRelation.
func (mr *MockStateManagerMockRecorder) RemoveRelation(arg0, arg1, arg2, arg3 any) *MockStateManagerRemoveRelationCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RemoveRelation", reflect.TypeOf((*MockStateManager)(nil).RemoveRelation), arg0, arg1, arg2, arg3)
	return &MockStateManagerRemoveRelationCall{Call: call}
}

// MockStateManagerRemoveRelationCall wrap *gomock.Call
type MockStateManagerRemoveRelationCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockStateManagerRemoveRelationCall) Return(arg0 error) *MockStateManagerRemoveRelationCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockStateManagerRemoveRelationCall) Do(f func(context.Context, int, relation.UnitGetter, map[string]bool) error) *MockStateManagerRemoveRelationCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockStateManagerRemoveRelationCall) DoAndReturn(f func(context.Context, int, relation.UnitGetter, map[string]bool) error) *MockStateManagerRemoveRelationCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// SetRelation mocks base method.
func (m *MockStateManager) SetRelation(arg0 context.Context, arg1 *relation.State) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "SetRelation", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// SetRelation indicates an expected call of SetRelation.
func (mr *MockStateManagerMockRecorder) SetRelation(arg0, arg1 any) *MockStateManagerSetRelationCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SetRelation", reflect.TypeOf((*MockStateManager)(nil).SetRelation), arg0, arg1)
	return &MockStateManagerSetRelationCall{Call: call}
}

// MockStateManagerSetRelationCall wrap *gomock.Call
type MockStateManagerSetRelationCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockStateManagerSetRelationCall) Return(arg0 error) *MockStateManagerSetRelationCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockStateManagerSetRelationCall) Do(f func(context.Context, *relation.State) error) *MockStateManagerSetRelationCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockStateManagerSetRelationCall) DoAndReturn(f func(context.Context, *relation.State) error) *MockStateManagerSetRelationCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}
