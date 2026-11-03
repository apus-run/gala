package casbin

import (
	"fmt"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
)

// Possession specifies which action scope a route accepts. Zero is invalid.
// Possession 指定路由接受的动作范围。零值非法。
type Possession uint8

const (
	// PossessionAny grants access regardless of ownership | 任意归属均可访问.
	PossessionAny Possession = iota + 1
	// PossessionOwn grants access only when the subject owns the resource | 仅资源归属者可访问.
	PossessionOwn
	// PossessionOwnOrAny grants access if any or own policy matches | any 或 own 策略命中即可访问.
	PossessionOwnOrAny
)
const (
	// ActionCreate is the create action | 创建动作.
	ActionCreate = "create"
	// ActionRead is the read action | 读取动作.
	ActionRead = "read"
	// ActionUpdate is the update action | 更新动作.
	ActionUpdate = "update"
	// ActionDelete is the delete action | 删除动作.
	ActionDelete = "delete"
)

// Access requires a policy grant, and for own access also an ownership check.
// IsOwn must use the same authenticated identity and resource that the handler
// will operate on. For mutable ownership, enforce the owner predicate again in
// the application's database operation/transaction to avoid check/use races.
// Access 要求策略授权；own 访问还需归属校验。IsOwn 必须使用与 handler 将操作
// 的同一身份和资源。归属可变时，请在应用的数据库操作/事务中再次执行归属判定，
// 避免 check/use 竞态。
type Access struct {
	Resource   string
	Action     string
	Possession Possession
	IsOwn      func(*gin.Context) (bool, error)
}

// RequiresAccess evaluates action:any OR (action:own AND IsOwn) as selected by
// Possession. It uses ValidationRule; PermissionParser is not invoked. Empty
// requirements skip identity lookup, matching RequiresPermissions.
// RequiresAccess 按 Possession 求值 action:any 或 (action:own AND IsOwn)。
// 使用 ValidationRule；不调用 PermissionParser。空需求跳过身份读取，
// 与 RequiresPermissions 一致。
func (b *Builder) RequiresAccess(accesses []Access, opts ...Option) (gin.HandlerFunc, error) {
	cfg, err := b.normalized()
	if err != nil {
		return nil, err
	}
	options := NewOptions(opts...)
	if err := options.Validate(); err != nil {
		return nil, err
	}
	accesses = slices.Clone(accesses)
	for i, a := range accesses {
		var problem string
		switch {
		case strings.TrimSpace(a.Resource) == "":
			problem = "empty resource"
		case strings.TrimSpace(a.Action) == "" || strings.Contains(a.Action, ":"):
			problem = "action must be non-empty and contain no colon"
		case a.Possession < PossessionAny || a.Possession > PossessionOwnOrAny:
			problem = "invalid possession"
		case a.Possession == PossessionAny && a.IsOwn != nil:
			problem = "any must not have IsOwn"
		case a.Possession != PossessionAny && a.IsOwn == nil:
			problem = "own requires IsOwn"
		}
		if problem != "" {
			return nil, fmt.Errorf("casbin middleware: access[%d]: %s", i, problem)
		}
	}
	return cfg.handler(len(accesses) == 0, func(c *gin.Context, subject string) (bool, error) {
		for _, a := range accesses {
			allowed, err := a.check(c, cfg.enforcer, subject)
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

func (a Access) check(c *gin.Context, e Enforcer, subject string) (bool, error) {
	if a.Possession != PossessionOwn {
		allowed, err := e.Enforce(subject, a.Resource, a.Action+":any")
		if err != nil || allowed || a.Possession == PossessionAny {
			return allowed, err
		}
	}
	allowed, err := e.Enforce(subject, a.Resource, a.Action+":own")
	if err != nil || !allowed {
		return false, err
	}
	return a.IsOwn(c)
}
