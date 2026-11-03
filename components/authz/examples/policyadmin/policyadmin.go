// Package policyadmin demonstrates the application-owned policy write path:
// the app manages casbin_rule rows itself and calls Authorizer.SyncPolicy so
// every instance reloads. authz ships no write API by design; validation,
// transactions and audit belong in this layer, not in the component.
// Package policyadmin 演示应用自持的策略写入路径：应用自己管理 casbin_rule
// 行并调用 Authorizer.SyncPolicy 让所有实例重载。authz 有意不提供写 API；
// 校验、事务与审计属于这一层，不属于组件。
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
// PolicyAdmin 是应用唯一的策略写入者。下面的“先查后插”假设每个策略库只有
// 一个写入者；多写入者并发时应改用唯一索引。每个方法都会立即同步；
// 批量写入的调用者可以一次提交全部改动，最后只调一次 SyncPolicy。
type PolicyAdmin struct {
	db *gorm.DB
	az *authz.Authorizer
}

// New leaves db and az application-owned | New 中 db 与 az 仍归应用持有。
func New(db *gorm.DB, az *authz.Authorizer) *PolicyAdmin {
	return &PolicyAdmin{db: db, az: az}
}

// AddPolicy inserts one p rule (sub → obj → act), idempotently | AddPolicy 幂等插入一条 p 规则（sub → obj → act）。
func (a *PolicyAdmin) AddPolicy(ctx context.Context, sub, obj, act string) error {
	rule := &gormadapter.CasbinRule{Ptype: "p", V0: sub, V1: obj, V2: act}
	cond := "ptype = ? AND v0 = ? AND v1 = ? AND v2 = ?"
	if err := a.db.WithContext(ctx).Where(cond, rule.Ptype, rule.V0, rule.V1, rule.V2).FirstOrCreate(rule).Error; err != nil {
		return err
	}
	return a.az.SyncPolicy()
}

// RemovePolicy deletes exactly one p rule; sibling rules survive | RemovePolicy 恰好删除一条 p 规则，其余规则保留。
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
// AddRoleForUser 插入一条 g 规则（user → role）；
// 角色继承链（g, role, role）用同样的行结构。
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
// RemoveRoleForUser 恰好删除一条 g 规则：用户只失去这一个授权，
// 而角色本身、角色的 p 规则及用户的其他角色都保留。
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
// DeletePermissionsForUser 一次写入删除所有主体为 sub 的 p 规则。
// “主体”的含义由模型决定：若 p = role, obj, act，则 sub 是角色名而非用户
// ——casbin 自己的 API 命名有误导性。
func (a *PolicyAdmin) DeletePermissionsForUser(ctx context.Context, sub string) error {
	res := a.db.WithContext(ctx).Where("ptype = ? AND v0 = ?", "p", sub).Delete(&gormadapter.CasbinRule{})
	if res.Error != nil {
		return res.Error
	}
	return a.az.SyncPolicy()
}
