// Code generated by MockGen. DO NOT EDIT.
// Source: service_resolver.go
//
// Generated by this command:
//
//	mockgen -package resolver -source service_resolver.go -destination service_resolver_mock.go
//

// Package resolver is a generated GoMock package.
package resolver

import (
	reflect "reflect"

	gomock "go.uber.org/mock/gomock"
)

// MockServiceResolver is a mock of ServiceResolver interface.
type MockServiceResolver struct {
	ctrl     *gomock.Controller
	recorder *MockServiceResolverMockRecorder
	isgomock struct{}
}

// MockServiceResolverMockRecorder is the mock recorder for MockServiceResolver.
type MockServiceResolverMockRecorder struct {
	mock *MockServiceResolver
}

// NewMockServiceResolver creates a new mock instance.
func NewMockServiceResolver(ctrl *gomock.Controller) *MockServiceResolver {
	mock := &MockServiceResolver{ctrl: ctrl}
	mock.recorder = &MockServiceResolverMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockServiceResolver) EXPECT() *MockServiceResolverMockRecorder {
	return m.recorder
}

// Resolve mocks base method.
func (m *MockServiceResolver) Resolve(service string) []string {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Resolve", service)
	ret0, _ := ret[0].([]string)
	return ret0
}

// Resolve indicates an expected call of Resolve.
func (mr *MockServiceResolverMockRecorder) Resolve(service any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Resolve", reflect.TypeOf((*MockServiceResolver)(nil).Resolve), service)
}
