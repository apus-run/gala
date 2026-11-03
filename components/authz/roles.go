package authz

import "slices"

// GetRolesForUser returns a copy of the subject's direct roles under the enforcer's
// read lock. It does not expand role inheritance. Optional domain semantics are
// Casbin's.
// GetRolesForUser 在执行器读锁下返回主体直接角色的副本，
// 不展开角色继承；可选 domain 的语义由 Casbin 决定。
func (a *Authorizer) GetRolesForUser(name string, domain ...string) ([]string, error) {
	if err := a.policyFresh(); err != nil {
		return nil, err
	}
	e := a.engine
	// v3.10.0 inherits this method from Enforcer rather than wrapping it with
	// SyncedEnforcer's lock. Synchronize with policy and role updates.
	// v3.10.0 里该方法继承自 Enforcer，没有包上 SyncedEnforcer 的锁，
	// 需自行与策略及角色更新同步。
	lock := e.GetLock()
	lock.RLock()
	defer lock.RUnlock()
	roles, err := e.Enforcer.GetRolesForUser(name, domain...)
	return slices.Clone(roles), err
}
