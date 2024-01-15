// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/juju/juju/internal/worker/uniter/operation (interfaces: Operation,Factory,Callbacks)
//
// Generated by this command:
//
//	mockgen -package mocks -destination mocks/interface_mock.go github.com/juju/juju/internal/worker/uniter/operation Operation,Factory,Callbacks
//

// Package mocks is a generated GoMock package.
package mocks

import (
	context "context"
	reflect "reflect"

	model "github.com/juju/juju/core/model"
	charm "github.com/juju/juju/internal/worker/uniter/charm"
	hook "github.com/juju/juju/internal/worker/uniter/hook"
	operation "github.com/juju/juju/internal/worker/uniter/operation"
	remotestate "github.com/juju/juju/internal/worker/uniter/remotestate"
	context0 "github.com/juju/juju/internal/worker/uniter/runner/context"
	gomock "go.uber.org/mock/gomock"
)

// MockOperation is a mock of Operation interface.
type MockOperation struct {
	ctrl     *gomock.Controller
	recorder *MockOperationMockRecorder
}

// MockOperationMockRecorder is the mock recorder for MockOperation.
type MockOperationMockRecorder struct {
	mock *MockOperation
}

// NewMockOperation creates a new mock instance.
func NewMockOperation(ctrl *gomock.Controller) *MockOperation {
	mock := &MockOperation{ctrl: ctrl}
	mock.recorder = &MockOperationMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockOperation) EXPECT() *MockOperationMockRecorder {
	return m.recorder
}

// Commit mocks base method.
func (m *MockOperation) Commit(arg0 context.Context, arg1 operation.State) (*operation.State, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Commit", arg0, arg1)
	ret0, _ := ret[0].(*operation.State)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Commit indicates an expected call of Commit.
func (mr *MockOperationMockRecorder) Commit(arg0, arg1 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Commit", reflect.TypeOf((*MockOperation)(nil).Commit), arg0, arg1)
}

// Execute mocks base method.
func (m *MockOperation) Execute(arg0 context.Context, arg1 operation.State) (*operation.State, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Execute", arg0, arg1)
	ret0, _ := ret[0].(*operation.State)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Execute indicates an expected call of Execute.
func (mr *MockOperationMockRecorder) Execute(arg0, arg1 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Execute", reflect.TypeOf((*MockOperation)(nil).Execute), arg0, arg1)
}

// ExecutionGroup mocks base method.
func (m *MockOperation) ExecutionGroup() string {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ExecutionGroup")
	ret0, _ := ret[0].(string)
	return ret0
}

// ExecutionGroup indicates an expected call of ExecutionGroup.
func (mr *MockOperationMockRecorder) ExecutionGroup() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ExecutionGroup", reflect.TypeOf((*MockOperation)(nil).ExecutionGroup))
}

// NeedsGlobalMachineLock mocks base method.
func (m *MockOperation) NeedsGlobalMachineLock() bool {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "NeedsGlobalMachineLock")
	ret0, _ := ret[0].(bool)
	return ret0
}

// NeedsGlobalMachineLock indicates an expected call of NeedsGlobalMachineLock.
func (mr *MockOperationMockRecorder) NeedsGlobalMachineLock() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "NeedsGlobalMachineLock", reflect.TypeOf((*MockOperation)(nil).NeedsGlobalMachineLock))
}

// Prepare mocks base method.
func (m *MockOperation) Prepare(arg0 context.Context, arg1 operation.State) (*operation.State, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Prepare", arg0, arg1)
	ret0, _ := ret[0].(*operation.State)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Prepare indicates an expected call of Prepare.
func (mr *MockOperationMockRecorder) Prepare(arg0, arg1 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Prepare", reflect.TypeOf((*MockOperation)(nil).Prepare), arg0, arg1)
}

// RemoteStateChanged mocks base method.
func (m *MockOperation) RemoteStateChanged(arg0 remotestate.Snapshot) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "RemoteStateChanged", arg0)
}

// RemoteStateChanged indicates an expected call of RemoteStateChanged.
func (mr *MockOperationMockRecorder) RemoteStateChanged(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RemoteStateChanged", reflect.TypeOf((*MockOperation)(nil).RemoteStateChanged), arg0)
}

// String mocks base method.
func (m *MockOperation) String() string {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "String")
	ret0, _ := ret[0].(string)
	return ret0
}

// String indicates an expected call of String.
func (mr *MockOperationMockRecorder) String() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "String", reflect.TypeOf((*MockOperation)(nil).String))
}

// MockFactory is a mock of Factory interface.
type MockFactory struct {
	ctrl     *gomock.Controller
	recorder *MockFactoryMockRecorder
}

// MockFactoryMockRecorder is the mock recorder for MockFactory.
type MockFactoryMockRecorder struct {
	mock *MockFactory
}

// NewMockFactory creates a new mock instance.
func NewMockFactory(ctrl *gomock.Controller) *MockFactory {
	mock := &MockFactory{ctrl: ctrl}
	mock.recorder = &MockFactoryMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockFactory) EXPECT() *MockFactoryMockRecorder {
	return m.recorder
}

// NewAcceptLeadership mocks base method.
func (m *MockFactory) NewAcceptLeadership() (operation.Operation, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "NewAcceptLeadership")
	ret0, _ := ret[0].(operation.Operation)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// NewAcceptLeadership indicates an expected call of NewAcceptLeadership.
func (mr *MockFactoryMockRecorder) NewAcceptLeadership() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "NewAcceptLeadership", reflect.TypeOf((*MockFactory)(nil).NewAcceptLeadership))
}

// NewAction mocks base method.
func (m *MockFactory) NewAction(arg0 string) (operation.Operation, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "NewAction", arg0)
	ret0, _ := ret[0].(operation.Operation)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// NewAction indicates an expected call of NewAction.
func (mr *MockFactoryMockRecorder) NewAction(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "NewAction", reflect.TypeOf((*MockFactory)(nil).NewAction), arg0)
}

// NewCommands mocks base method.
func (m *MockFactory) NewCommands(arg0 operation.CommandArgs, arg1 operation.CommandResponseFunc) (operation.Operation, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "NewCommands", arg0, arg1)
	ret0, _ := ret[0].(operation.Operation)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// NewCommands indicates an expected call of NewCommands.
func (mr *MockFactoryMockRecorder) NewCommands(arg0, arg1 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "NewCommands", reflect.TypeOf((*MockFactory)(nil).NewCommands), arg0, arg1)
}

// NewFailAction mocks base method.
func (m *MockFactory) NewFailAction(arg0 string) (operation.Operation, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "NewFailAction", arg0)
	ret0, _ := ret[0].(operation.Operation)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// NewFailAction indicates an expected call of NewFailAction.
func (mr *MockFactoryMockRecorder) NewFailAction(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "NewFailAction", reflect.TypeOf((*MockFactory)(nil).NewFailAction), arg0)
}

// NewInstall mocks base method.
func (m *MockFactory) NewInstall(arg0 string) (operation.Operation, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "NewInstall", arg0)
	ret0, _ := ret[0].(operation.Operation)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// NewInstall indicates an expected call of NewInstall.
func (mr *MockFactoryMockRecorder) NewInstall(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "NewInstall", reflect.TypeOf((*MockFactory)(nil).NewInstall), arg0)
}

// NewNoOpFinishUpgradeSeries mocks base method.
func (m *MockFactory) NewNoOpFinishUpgradeSeries() (operation.Operation, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "NewNoOpFinishUpgradeSeries")
	ret0, _ := ret[0].(operation.Operation)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// NewNoOpFinishUpgradeSeries indicates an expected call of NewNoOpFinishUpgradeSeries.
func (mr *MockFactoryMockRecorder) NewNoOpFinishUpgradeSeries() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "NewNoOpFinishUpgradeSeries", reflect.TypeOf((*MockFactory)(nil).NewNoOpFinishUpgradeSeries))
}

// NewNoOpSecretsRemoved mocks base method.
func (m *MockFactory) NewNoOpSecretsRemoved(arg0 []string) (operation.Operation, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "NewNoOpSecretsRemoved", arg0)
	ret0, _ := ret[0].(operation.Operation)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// NewNoOpSecretsRemoved indicates an expected call of NewNoOpSecretsRemoved.
func (mr *MockFactoryMockRecorder) NewNoOpSecretsRemoved(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "NewNoOpSecretsRemoved", reflect.TypeOf((*MockFactory)(nil).NewNoOpSecretsRemoved), arg0)
}

// NewRemoteInit mocks base method.
func (m *MockFactory) NewRemoteInit(arg0 remotestate.ContainerRunningStatus) (operation.Operation, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "NewRemoteInit", arg0)
	ret0, _ := ret[0].(operation.Operation)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// NewRemoteInit indicates an expected call of NewRemoteInit.
func (mr *MockFactoryMockRecorder) NewRemoteInit(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "NewRemoteInit", reflect.TypeOf((*MockFactory)(nil).NewRemoteInit), arg0)
}

// NewResignLeadership mocks base method.
func (m *MockFactory) NewResignLeadership() (operation.Operation, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "NewResignLeadership")
	ret0, _ := ret[0].(operation.Operation)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// NewResignLeadership indicates an expected call of NewResignLeadership.
func (mr *MockFactoryMockRecorder) NewResignLeadership() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "NewResignLeadership", reflect.TypeOf((*MockFactory)(nil).NewResignLeadership))
}

// NewResolvedUpgrade mocks base method.
func (m *MockFactory) NewResolvedUpgrade(arg0 string) (operation.Operation, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "NewResolvedUpgrade", arg0)
	ret0, _ := ret[0].(operation.Operation)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// NewResolvedUpgrade indicates an expected call of NewResolvedUpgrade.
func (mr *MockFactoryMockRecorder) NewResolvedUpgrade(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "NewResolvedUpgrade", reflect.TypeOf((*MockFactory)(nil).NewResolvedUpgrade), arg0)
}

// NewRevertUpgrade mocks base method.
func (m *MockFactory) NewRevertUpgrade(arg0 string) (operation.Operation, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "NewRevertUpgrade", arg0)
	ret0, _ := ret[0].(operation.Operation)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// NewRevertUpgrade indicates an expected call of NewRevertUpgrade.
func (mr *MockFactoryMockRecorder) NewRevertUpgrade(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "NewRevertUpgrade", reflect.TypeOf((*MockFactory)(nil).NewRevertUpgrade), arg0)
}

// NewRunHook mocks base method.
func (m *MockFactory) NewRunHook(arg0 hook.Info) (operation.Operation, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "NewRunHook", arg0)
	ret0, _ := ret[0].(operation.Operation)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// NewRunHook indicates an expected call of NewRunHook.
func (mr *MockFactoryMockRecorder) NewRunHook(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "NewRunHook", reflect.TypeOf((*MockFactory)(nil).NewRunHook), arg0)
}

// NewSkipHook mocks base method.
func (m *MockFactory) NewSkipHook(arg0 hook.Info) (operation.Operation, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "NewSkipHook", arg0)
	ret0, _ := ret[0].(operation.Operation)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// NewSkipHook indicates an expected call of NewSkipHook.
func (mr *MockFactoryMockRecorder) NewSkipHook(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "NewSkipHook", reflect.TypeOf((*MockFactory)(nil).NewSkipHook), arg0)
}

// NewSkipRemoteInit mocks base method.
func (m *MockFactory) NewSkipRemoteInit(arg0 bool) (operation.Operation, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "NewSkipRemoteInit", arg0)
	ret0, _ := ret[0].(operation.Operation)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// NewSkipRemoteInit indicates an expected call of NewSkipRemoteInit.
func (mr *MockFactoryMockRecorder) NewSkipRemoteInit(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "NewSkipRemoteInit", reflect.TypeOf((*MockFactory)(nil).NewSkipRemoteInit), arg0)
}

// NewUpgrade mocks base method.
func (m *MockFactory) NewUpgrade(arg0 string) (operation.Operation, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "NewUpgrade", arg0)
	ret0, _ := ret[0].(operation.Operation)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// NewUpgrade indicates an expected call of NewUpgrade.
func (mr *MockFactoryMockRecorder) NewUpgrade(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "NewUpgrade", reflect.TypeOf((*MockFactory)(nil).NewUpgrade), arg0)
}

// MockCallbacks is a mock of Callbacks interface.
type MockCallbacks struct {
	ctrl     *gomock.Controller
	recorder *MockCallbacksMockRecorder
}

// MockCallbacksMockRecorder is the mock recorder for MockCallbacks.
type MockCallbacksMockRecorder struct {
	mock *MockCallbacks
}

// NewMockCallbacks creates a new mock instance.
func NewMockCallbacks(ctrl *gomock.Controller) *MockCallbacks {
	mock := &MockCallbacks{ctrl: ctrl}
	mock.recorder = &MockCallbacksMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockCallbacks) EXPECT() *MockCallbacksMockRecorder {
	return m.recorder
}

// ActionStatus mocks base method.
func (m *MockCallbacks) ActionStatus(arg0 string) (string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ActionStatus", arg0)
	ret0, _ := ret[0].(string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ActionStatus indicates an expected call of ActionStatus.
func (mr *MockCallbacksMockRecorder) ActionStatus(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ActionStatus", reflect.TypeOf((*MockCallbacks)(nil).ActionStatus), arg0)
}

// CommitHook mocks base method.
func (m *MockCallbacks) CommitHook(arg0 hook.Info) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CommitHook", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// CommitHook indicates an expected call of CommitHook.
func (mr *MockCallbacksMockRecorder) CommitHook(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CommitHook", reflect.TypeOf((*MockCallbacks)(nil).CommitHook), arg0)
}

// FailAction mocks base method.
func (m *MockCallbacks) FailAction(arg0, arg1 string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "FailAction", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// FailAction indicates an expected call of FailAction.
func (mr *MockCallbacksMockRecorder) FailAction(arg0, arg1 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "FailAction", reflect.TypeOf((*MockCallbacks)(nil).FailAction), arg0, arg1)
}

// GetArchiveInfo mocks base method.
func (m *MockCallbacks) GetArchiveInfo(arg0 string) (charm.BundleInfo, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetArchiveInfo", arg0)
	ret0, _ := ret[0].(charm.BundleInfo)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetArchiveInfo indicates an expected call of GetArchiveInfo.
func (mr *MockCallbacksMockRecorder) GetArchiveInfo(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetArchiveInfo", reflect.TypeOf((*MockCallbacks)(nil).GetArchiveInfo), arg0)
}

// NotifyHookCompleted mocks base method.
func (m *MockCallbacks) NotifyHookCompleted(arg0 string, arg1 context0.Context) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "NotifyHookCompleted", arg0, arg1)
}

// NotifyHookCompleted indicates an expected call of NotifyHookCompleted.
func (mr *MockCallbacksMockRecorder) NotifyHookCompleted(arg0, arg1 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "NotifyHookCompleted", reflect.TypeOf((*MockCallbacks)(nil).NotifyHookCompleted), arg0, arg1)
}

// NotifyHookFailed mocks base method.
func (m *MockCallbacks) NotifyHookFailed(arg0 string, arg1 context0.Context) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "NotifyHookFailed", arg0, arg1)
}

// NotifyHookFailed indicates an expected call of NotifyHookFailed.
func (mr *MockCallbacksMockRecorder) NotifyHookFailed(arg0, arg1 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "NotifyHookFailed", reflect.TypeOf((*MockCallbacks)(nil).NotifyHookFailed), arg0, arg1)
}

// PrepareHook mocks base method.
func (m *MockCallbacks) PrepareHook(arg0 hook.Info) (string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "PrepareHook", arg0)
	ret0, _ := ret[0].(string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// PrepareHook indicates an expected call of PrepareHook.
func (mr *MockCallbacksMockRecorder) PrepareHook(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "PrepareHook", reflect.TypeOf((*MockCallbacks)(nil).PrepareHook), arg0)
}

// RemoteInit mocks base method.
func (m *MockCallbacks) RemoteInit(arg0 remotestate.ContainerRunningStatus, arg1 <-chan struct{}) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "RemoteInit", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// RemoteInit indicates an expected call of RemoteInit.
func (mr *MockCallbacksMockRecorder) RemoteInit(arg0, arg1 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RemoteInit", reflect.TypeOf((*MockCallbacks)(nil).RemoteInit), arg0, arg1)
}

// SecretsRemoved mocks base method.
func (m *MockCallbacks) SecretsRemoved(arg0 []string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "SecretsRemoved", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// SecretsRemoved indicates an expected call of SecretsRemoved.
func (mr *MockCallbacksMockRecorder) SecretsRemoved(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SecretsRemoved", reflect.TypeOf((*MockCallbacks)(nil).SecretsRemoved), arg0)
}

// SetCurrentCharm mocks base method.
func (m *MockCallbacks) SetCurrentCharm(arg0 string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "SetCurrentCharm", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// SetCurrentCharm indicates an expected call of SetCurrentCharm.
func (mr *MockCallbacksMockRecorder) SetCurrentCharm(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SetCurrentCharm", reflect.TypeOf((*MockCallbacks)(nil).SetCurrentCharm), arg0)
}

// SetExecutingStatus mocks base method.
func (m *MockCallbacks) SetExecutingStatus(arg0 string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "SetExecutingStatus", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// SetExecutingStatus indicates an expected call of SetExecutingStatus.
func (mr *MockCallbacksMockRecorder) SetExecutingStatus(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SetExecutingStatus", reflect.TypeOf((*MockCallbacks)(nil).SetExecutingStatus), arg0)
}

// SetSecretRotated mocks base method.
func (m *MockCallbacks) SetSecretRotated(arg0 string, arg1 int) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "SetSecretRotated", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// SetSecretRotated indicates an expected call of SetSecretRotated.
func (mr *MockCallbacksMockRecorder) SetSecretRotated(arg0, arg1 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SetSecretRotated", reflect.TypeOf((*MockCallbacks)(nil).SetSecretRotated), arg0, arg1)
}

// SetUpgradeSeriesStatus mocks base method.
func (m *MockCallbacks) SetUpgradeSeriesStatus(arg0 model.UpgradeSeriesStatus, arg1 string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "SetUpgradeSeriesStatus", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// SetUpgradeSeriesStatus indicates an expected call of SetUpgradeSeriesStatus.
func (mr *MockCallbacksMockRecorder) SetUpgradeSeriesStatus(arg0, arg1 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SetUpgradeSeriesStatus", reflect.TypeOf((*MockCallbacks)(nil).SetUpgradeSeriesStatus), arg0, arg1)
}