package casbin

import (
	"fmt"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
)

// Possession specifies which action scope a route accepts. Zero is invalid.
type Possession uint8

const (
	PossessionAny Possession = iota + 1
	PossessionOwn
	PossessionOwnOrAny
)
const (
	ActionCreate = "create"
	ActionRead   = "read"
	ActionUpdate = "update"
	ActionDelete = "delete"
)

// Access requires a policy grant, and for own access also an ownership check.
// IsOwn must use the same authenticated identity and resource that the handler
// will operate on. For mutable ownership, enforce the owner predicate again in
// the application's database operation/transaction to avoid check/use races.
type Access struct {
	Resource   string
	Action     string
	Possession Possession
	IsOwn      func(*gin.Context) (bool, error)
}

// RequiresAccess evaluates action:any OR (action:own AND IsOwn) as selected by
// Possession. It uses ValidationRule; PermissionParser is not invoked. Empty
// requirements skip identity lookup, matching RequiresPermissions.
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
