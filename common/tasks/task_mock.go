// Code generated by MockGen. DO NOT EDIT.
// Source: task.go
//
// Generated by this command:
//
//	mockgen -package tasks -source task.go -destination task_mock.go
//

// Package tasks is a generated GoMock package.
package tasks

import (
	context "context"
	reflect "reflect"

	backoff "go.temporal.io/server/common/backoff"
	gomock "go.uber.org/mock/gomock"
)

// MockRunnable is a mock of Runnable interface.
type MockRunnable struct {
	ctrl     *gomock.Controller
	recorder *MockRunnableMockRecorder
	isgomock struct{}
}

// MockRunnableMockRecorder is the mock recorder for MockRunnable.
type MockRunnableMockRecorder struct {
	mock *MockRunnable
}

// NewMockRunnable creates a new mock instance.
func NewMockRunnable(ctrl *gomock.Controller) *MockRunnable {
	mock := &MockRunnable{ctrl: ctrl}
	mock.recorder = &MockRunnableMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockRunnable) EXPECT() *MockRunnableMockRecorder {
	return m.recorder
}

// Abort mocks base method.
func (m *MockRunnable) Abort() {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "Abort")
}

// Abort indicates an expected call of Abort.
func (mr *MockRunnableMockRecorder) Abort() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Abort", reflect.TypeOf((*MockRunnable)(nil).Abort))
}

// Run mocks base method.
func (m *MockRunnable) Run(arg0 context.Context) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "Run", arg0)
}

// Run indicates an expected call of Run.
func (mr *MockRunnableMockRecorder) Run(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Run", reflect.TypeOf((*MockRunnable)(nil).Run), arg0)
}

// MockTask is a mock of Task interface.
type MockTask struct {
	ctrl     *gomock.Controller
	recorder *MockTaskMockRecorder
	isgomock struct{}
}

// MockTaskMockRecorder is the mock recorder for MockTask.
type MockTaskMockRecorder struct {
	mock *MockTask
}

// NewMockTask creates a new mock instance.
func NewMockTask(ctrl *gomock.Controller) *MockTask {
	mock := &MockTask{ctrl: ctrl}
	mock.recorder = &MockTaskMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockTask) EXPECT() *MockTaskMockRecorder {
	return m.recorder
}

// Abort mocks base method.
func (m *MockTask) Abort() {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "Abort")
}

// Abort indicates an expected call of Abort.
func (mr *MockTaskMockRecorder) Abort() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Abort", reflect.TypeOf((*MockTask)(nil).Abort))
}

// Ack mocks base method.
func (m *MockTask) Ack() {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "Ack")
}

// Ack indicates an expected call of Ack.
func (mr *MockTaskMockRecorder) Ack() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Ack", reflect.TypeOf((*MockTask)(nil).Ack))
}

// Cancel mocks base method.
func (m *MockTask) Cancel() {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "Cancel")
}

// Cancel indicates an expected call of Cancel.
func (mr *MockTaskMockRecorder) Cancel() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Cancel", reflect.TypeOf((*MockTask)(nil).Cancel))
}

// Execute mocks base method.
func (m *MockTask) Execute() error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Execute")
	ret0, _ := ret[0].(error)
	return ret0
}

// Execute indicates an expected call of Execute.
func (mr *MockTaskMockRecorder) Execute() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Execute", reflect.TypeOf((*MockTask)(nil).Execute))
}

// HandleErr mocks base method.
func (m *MockTask) HandleErr(err error) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "HandleErr", err)
	ret0, _ := ret[0].(error)
	return ret0
}

// HandleErr indicates an expected call of HandleErr.
func (mr *MockTaskMockRecorder) HandleErr(err any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "HandleErr", reflect.TypeOf((*MockTask)(nil).HandleErr), err)
}

// IsRetryableError mocks base method.
func (m *MockTask) IsRetryableError(err error) bool {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "IsRetryableError", err)
	ret0, _ := ret[0].(bool)
	return ret0
}

// IsRetryableError indicates an expected call of IsRetryableError.
func (mr *MockTaskMockRecorder) IsRetryableError(err any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "IsRetryableError", reflect.TypeOf((*MockTask)(nil).IsRetryableError), err)
}

// Nack mocks base method.
func (m *MockTask) Nack(err error) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "Nack", err)
}

// Nack indicates an expected call of Nack.
func (mr *MockTaskMockRecorder) Nack(err any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Nack", reflect.TypeOf((*MockTask)(nil).Nack), err)
}

// Reschedule mocks base method.
func (m *MockTask) Reschedule() {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "Reschedule")
}

// Reschedule indicates an expected call of Reschedule.
func (mr *MockTaskMockRecorder) Reschedule() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Reschedule", reflect.TypeOf((*MockTask)(nil).Reschedule))
}

// RetryPolicy mocks base method.
func (m *MockTask) RetryPolicy() backoff.RetryPolicy {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "RetryPolicy")
	ret0, _ := ret[0].(backoff.RetryPolicy)
	return ret0
}

// RetryPolicy indicates an expected call of RetryPolicy.
func (mr *MockTaskMockRecorder) RetryPolicy() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RetryPolicy", reflect.TypeOf((*MockTask)(nil).RetryPolicy))
}

// State mocks base method.
func (m *MockTask) State() State {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "State")
	ret0, _ := ret[0].(State)
	return ret0
}

// State indicates an expected call of State.
func (mr *MockTaskMockRecorder) State() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "State", reflect.TypeOf((*MockTask)(nil).State))
}
