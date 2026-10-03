package casbin

import (
	"errors"
	"slices"

	"github.com/gin-gonic/gin"
)

// RoleGetter is required only by RequiresRoles. GetRolesForUser returns direct
// roles, not inherited roles. RequiresRoles uses the default (unscoped) domain;
// applications needing a domain can inject a domain-bound implementation.
type RoleGetter interface {
	GetRolesForUser(name string, domain ...string) ([]string, error)
}

// RoutePermission authorizes subject, URL.Path, and HTTP method, or the fields
// supplied by SetRequestValues. Query parameters are not part of URL.Path.
func (b *Builder) RoutePermission() (gin.HandlerFunc, error) {
	cfg, err := b.normalized()
	if err != nil {
		return nil, err
	}
	return cfg.handler(false, func(c *gin.Context, subject string) (bool, error) {
		values, err := cfg.requestValues(c, subject)
		if err != nil {
			return false, err
		}
		if len(values) == 0 {
			return false, errors.New("casbin middleware: empty request values")
		}
		return cfg.enforcer.Enforce(values...)
	}), nil
}

// RequiresPermissions checks a list with MatchAllRule by default. Each permission
// is parsed independently and passed to Enforce with the subject prepended.
// Evaluation stops on the first decisive result or error, in input order.
// An empty list allows the request without calling Lookup or Enforce.
func (b *Builder) RequiresPermissions(permissions []string, opts ...Option) (gin.HandlerFunc, error) {
	cfg, err := b.normalized()
	if err != nil {
		return nil, err
	}
	options := NewOptions(opts...)
	if err := options.Validate(); err != nil {
		return nil, err
	}
	permissions = slices.Clone(permissions)
	return cfg.handler(len(permissions) == 0, func(_ *gin.Context, subject string) (bool, error) {
		for _, permission := range permissions {
			fields := options.permissionParser(permission)
			values := make([]any, len(fields)+1)
			values[0] = subject
			for i, field := range fields {
				values[i+1] = field
			}
			allowed, err := cfg.enforcer.Enforce(values...)
			if err != nil {
				return false, err
			}
			if options.validationRule == MatchAllRule && !allowed {
				return false, nil
			}
			if options.validationRule == AtLeastOneRule && allowed {
				return true, nil
			}
		}
		return options.validationRule == MatchAllRule, nil
	}), nil
}

// RequiresRoles checks direct roles returned by GetRolesForUser. It does not
// reinterpret a role as a permission or expand inheritance. An empty requirement
// list allows the request without looking up identity or roles.
func (b *Builder) RequiresRoles(roles []string, opts ...Option) (gin.HandlerFunc, error) {
	cfg, err := b.normalized()
	if err != nil {
		return nil, err
	}
	options := NewOptions(opts...)
	if err := options.Validate(); err != nil {
		return nil, err
	}
	getter, ok := cfg.enforcer.(RoleGetter)
	if len(roles) != 0 && (!ok || isNil(getter)) {
		return nil, errors.New("casbin middleware: Enforcer must implement GetRolesForUser for role authorization")
	}
	roles = slices.Clone(roles)
	return cfg.handler(len(roles) == 0, func(_ *gin.Context, subject string) (bool, error) {
		userRoles, err := getter.GetRolesForUser(subject)
		if err != nil {
			return false, err
		}
		for _, role := range roles {
			hasRole := slices.Contains(userRoles, role)
			if options.validationRule == MatchAllRule && !hasRole {
				return false, nil
			}
			if options.validationRule == AtLeastOneRule && hasRole {
				return true, nil
			}
		}
		return options.validationRule == MatchAllRule, nil
	}), nil
}
