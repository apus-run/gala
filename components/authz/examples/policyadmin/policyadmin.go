// Package policyadmin demonstrates the application-owned policy write path:
// the app manages casbin_rule rows itself and calls Authorizer.SyncPolicy so
// every instance reloads. authz ships no write API by design; validation,
// transactions and audit belong in this layer, not in the component.
package policyadmin

import (
	"context"

	"github.com/apus-run/gala/components/authz"
	gormadapter "github.com/apus-run/gala/components/authz/adapter/gorm"
	"gorm.io/gorm"
)

// PolicyAdmin is the application's single policy writer. The check-then-insert
// below assumes one writer per policy store; concurrent writers need a unique
// index instead. Each method syncs immediately; a caller batching several
// writes may commit them all and call SyncPolicy once at the end.
type PolicyAdmin struct {
	db *gorm.DB
	az *authz.Authorizer
}

// New leaves db and az application-owned.
func New(db *gorm.DB, az *authz.Authorizer) *PolicyAdmin {
	return &PolicyAdmin{db: db, az: az}
}

// AddPolicy inserts one p rule (sub → obj → act), idempotently.
func (a *PolicyAdmin) AddPolicy(ctx context.Context, sub, obj, act string) error {
	rule := &gormadapter.CasbinRule{Ptype: "p", V0: sub, V1: obj, V2: act}
	cond := "ptype = ? AND v0 = ? AND v1 = ? AND v2 = ?"
	if err := a.db.WithContext(ctx).Where(cond, rule.Ptype, rule.V0, rule.V1, rule.V2).FirstOrCreate(rule).Error; err != nil {
		return err
	}
	return a.az.SyncPolicy()
}

// RemovePolicy deletes exactly one p rule; sibling rules survive.
func (a *PolicyAdmin) RemovePolicy(ctx context.Context, sub, obj, act string) error {
	cond := "ptype = ? AND v0 = ? AND v1 = ? AND v2 = ?"
	res := a.db.WithContext(ctx).Where(cond, "p", sub, obj, act).Delete(&gormadapter.CasbinRule{})
	if res.Error != nil {
		return res.Error
	}
	return a.az.SyncPolicy()
}

// AddRoleForUser inserts one g rule (user → role). Role inheritance chains
// (g, role, role) use the same row shape.
func (a *PolicyAdmin) AddRoleForUser(ctx context.Context, user, role string) error {
	rule := &gormadapter.CasbinRule{Ptype: "g", V0: user, V1: role}
	cond := "ptype = ? AND v0 = ? AND v1 = ?"
	if err := a.db.WithContext(ctx).Where(cond, rule.Ptype, rule.V0, rule.V1).FirstOrCreate(rule).Error; err != nil {
		return err
	}
	return a.az.SyncPolicy()
}

// RemoveRoleForUser deletes exactly one g rule: the user loses this one
// assignment, while the role, its p rules and the user's other roles survive.
func (a *PolicyAdmin) RemoveRoleForUser(ctx context.Context, user, role string) error {
	res := a.db.WithContext(ctx).Where("ptype = ? AND v0 = ? AND v1 = ?", "g", user, role).Delete(&gormadapter.CasbinRule{})
	if res.Error != nil {
		return res.Error
	}
	return a.az.SyncPolicy()
}

// DeletePermissionsForUser removes every p rule whose subject is sub in one
// write. What "the subject" means is the model's decision: with p = role, obj,
// act, sub is a role name, not a user — casbin's own API name is misleading.
func (a *PolicyAdmin) DeletePermissionsForUser(ctx context.Context, sub string) error {
	res := a.db.WithContext(ctx).Where("ptype = ? AND v0 = ?", "p", sub).Delete(&gormadapter.CasbinRule{})
	if res.Error != nil {
		return res.Error
	}
	return a.az.SyncPolicy()
}
