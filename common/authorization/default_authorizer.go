package authorization

import (
	"context"

	"go.temporal.io/server/common/api"
)

type (
	defaultAuthorizer struct {
	}
)

var _ Authorizer = (*defaultAuthorizer)(nil)

// NewDefaultAuthorizer creates a default authorizer
func NewDefaultAuthorizer() Authorizer {
	return &defaultAuthorizer{}
}

var resultAllow = Result{Decision: DecisionAllow}
var resultDeny = Result{Decision: DecisionDeny}

// Authorize determines if an API call by given claims should be allowed or denied.
// Rules:
//
//	Health check APIs are allowed to everyone.
//	System Admin is allowed to access all APIs on all namespaces and cluster-level.
//	System Writer is allowed to access non admin APIs on all namespaces and cluster-level.
//	System Reader is allowed to access readonly APIs on all namespaces and cluster-level.
//	Namespace Admin is allowed to access all APIs on their namespaces.
//	Namespace Writer is allowed to access non admin APIs on their namespaces.
//	Namespace Reader is allowed to access non admin readonly APIs on their namespaces.
func (a *defaultAuthorizer) Authorize(_ context.Context, claims *Claims, target *CallTarget) (Result, error) {
	// APIs that are essentially read-only health checks with no sensitive information are
	// always allowed
	if IsHealthCheckAPI(target.APIName) {
		return resultAllow, nil
	}
	if claims == nil {
		return resultDeny, nil
	}

	metadata := api.GetMethodMetadata(target.APIName)

	var hasRole Role
	switch metadata.Scope {
	case api.ScopeCluster:
		hasRole = claims.System
	case api.ScopeNamespace:
		// Note: system-level claims apply across all namespaces.
		// Note: if claims.Namespace is nil or target.Namespace is not found, the lookup will return zero.
		hasRole = claims.System | claims.Namespaces[target.Namespace]
	default:
		return resultDeny, nil
	}

	if hasRole >= getRequiredRole(metadata.Access) {
		return resultAllow, nil
	}
	return resultDeny, nil
}

// Convert from api.Access to Role
func getRequiredRole(access api.Access) Role {
	switch access {
	case api.AccessReadOnly:
		return RoleReader
	case api.AccessWrite:
		return RoleWriter
	default:
		return RoleAdmin
	}
}
