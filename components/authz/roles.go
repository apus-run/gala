package authz

import "slices"

// GetRolesForUser returns a copy of the subject's direct roles under the enforcer's
// read lock. It does not expand role inheritance. Optional domain semantics are
// Casbin's.
func (a *Authorizer) GetRolesForUser(name string, domain ...string) ([]string, error) {
	e := a.engine
	// v3.10.0 inherits this method from Enforcer rather than wrapping it with
	// SyncedEnforcer's lock. Synchronize with policy and role updates.
	lock := e.GetLock()
	lock.RLock()
	defer lock.RUnlock()
	roles, err := e.Enforcer.GetRolesForUser(name, domain...)
	return slices.Clone(roles), err
}
