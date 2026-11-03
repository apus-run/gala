package casbin

import (
	"errors"
	"slices"

	"github.com/gin-gonic/gin"
)

// RoleGetter is required only by RequiresRoles. GetRolesForUser returns direct
// roles, not inherited roles. RequiresRoles uses the default (unscoped) domain;
// applications needing a domain can inject a domain-bound implementation.
// RoleGetter 仅 RequiresRoles 需要。GetRolesForUser 返回直接角色而非继承角色。
// RequiresRoles 使用默认（无 scope）domain；需要 domain 的应用可注入绑定
// domain 的实现。
type RoleGetter interface {
	GetRolesForUser(name string, domain ...string) ([]string, error)
}

// RoutePermission authorizes subject, URL.Path, and HTTP method, or the fields
// supplied by SetRequestValues. Query parameters are not part of URL.Path.
// RoutePermission 授权 subject、URL.Path 与 HTTP method（或 SetRequestValues
// 提供的字段）。查询参数不属于 URL.Path。
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
// RequiresPermissions 默认按 MatchAllRule 检查列表。每个权限独立解析并在
// subject 前置后传给 Enforce。按输入顺序求值，遇到首个决定性结果或错误即停。
// 空列表直接放行，不调用 Lookup 与 Enforce。
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
// RequiresRoles 检查 GetRolesForUser 返回的直接角色，不把角色重解释为权限，
// 也不展开继承。空需求列表直接放行，不读取身份与角色。
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
