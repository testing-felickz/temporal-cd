// Code generated by MockGen. DO NOT EDIT.
// Source: client.go
//
// Generated by this command:
//
//	mockgen -package connector -source client.go -destination client_mock.go
//

// Package connector is a generated GoMock package.
package connector

import (
	context "context"
	reflect "reflect"

	archiver "go.temporal.io/server/common/archiver"
	gomock "go.uber.org/mock/gomock"
)

// MockClient is a mock of Client interface.
type MockClient struct {
	ctrl     *gomock.Controller
	recorder *MockClientMockRecorder
	isgomock struct{}
}

// MockClientMockRecorder is the mock recorder for MockClient.
type MockClientMockRecorder struct {
	mock *MockClient
}

// NewMockClient creates a new mock instance.
func NewMockClient(ctrl *gomock.Controller) *MockClient {
	mock := &MockClient{ctrl: ctrl}
	mock.recorder = &MockClientMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockClient) EXPECT() *MockClientMockRecorder {
	return m.recorder
}

// Exist mocks base method.
func (m *MockClient) Exist(ctx context.Context, URI archiver.URI, fileName string) (bool, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Exist", ctx, URI, fileName)
	ret0, _ := ret[0].(bool)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Exist indicates an expected call of Exist.
func (mr *MockClientMockRecorder) Exist(ctx, URI, fileName any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Exist", reflect.TypeOf((*MockClient)(nil).Exist), ctx, URI, fileName)
}

// Get mocks base method.
func (m *MockClient) Get(ctx context.Context, URI archiver.URI, file string) ([]byte, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Get", ctx, URI, file)
	ret0, _ := ret[0].([]byte)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Get indicates an expected call of Get.
func (mr *MockClientMockRecorder) Get(ctx, URI, file any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Get", reflect.TypeOf((*MockClient)(nil).Get), ctx, URI, file)
}

// Query mocks base method.
func (m *MockClient) Query(ctx context.Context, URI archiver.URI, fileNamePrefix string) ([]string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Query", ctx, URI, fileNamePrefix)
	ret0, _ := ret[0].([]string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Query indicates an expected call of Query.
func (mr *MockClientMockRecorder) Query(ctx, URI, fileNamePrefix any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Query", reflect.TypeOf((*MockClient)(nil).Query), ctx, URI, fileNamePrefix)
}

// QueryWithFilters mocks base method.
func (m *MockClient) QueryWithFilters(ctx context.Context, URI archiver.URI, fileNamePrefix string, pageSize, offset int, filters []Precondition) ([]string, bool, int, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "QueryWithFilters", ctx, URI, fileNamePrefix, pageSize, offset, filters)
	ret0, _ := ret[0].([]string)
	ret1, _ := ret[1].(bool)
	ret2, _ := ret[2].(int)
	ret3, _ := ret[3].(error)
	return ret0, ret1, ret2, ret3
}

// QueryWithFilters indicates an expected call of QueryWithFilters.
func (mr *MockClientMockRecorder) QueryWithFilters(ctx, URI, fileNamePrefix, pageSize, offset, filters any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "QueryWithFilters", reflect.TypeOf((*MockClient)(nil).QueryWithFilters), ctx, URI, fileNamePrefix, pageSize, offset, filters)
}

// Upload mocks base method.
func (m *MockClient) Upload(ctx context.Context, URI archiver.URI, fileName string, file []byte) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Upload", ctx, URI, fileName, file)
	ret0, _ := ret[0].(error)
	return ret0
}

// Upload indicates an expected call of Upload.
func (mr *MockClientMockRecorder) Upload(ctx, URI, fileName, file any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Upload", reflect.TypeOf((*MockClient)(nil).Upload), ctx, URI, fileName, file)
}
