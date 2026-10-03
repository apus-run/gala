package authz

import (
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/casbin/casbin/v3/rbac"
)

const testDomainRoleModel = `[request_definition]
r = sub, dom, obj, act
[policy_definition]
p = sub, dom, obj, act
[role_definition]
g = _, _, _
[policy_effect]
e = some(where (p.eft == allow))
[matchers]
m = g(r.sub, p.sub, r.dom) && r.dom == p.dom && r.obj == p.obj && r.act == p.act`

func assertRoleSet(t *testing.T, got, want []string) {
	t.Helper()
	// Casbin does not guarantee role ordering; sort copies so the assertion
	// cannot change a slice whose ownership is being tested.
	actual, expected := slices.Clone(got), slices.Clone(want)
	slices.Sort(actual)
	slices.Sort(expected)
	if !slices.Equal(actual, expected) {
		t.Fatalf("roles=%v, want %v", got, want)
	}
}

func TestGetRolesForUserReturnsDirectRoles(t *testing.T) {
	a := roleTestAuthz(t, testAllowModel, `g, alice, editor
g, alice, auditor
g, alice, editor
g, editor, admin
g, bob, viewer
g, ALICE, operator`)
	for _, tc := range []struct {
		name string
		want []string
	}{
		{"alice", []string{"editor", "auditor"}},
		{"editor", []string{"admin"}},
		{"admin", nil},
		{"bob", []string{"viewer"}},
		{"ALICE", []string{"operator"}},
		{"unknown", nil},
		{"", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			roles, err := a.GetRolesForUser(tc.name)
			if err != nil {
				t.Fatal(err)
			}
			assertRoleSet(t, roles, tc.want)
		})
	}
}

func TestGetRolesForUserWithoutGroupingPolicies(t *testing.T) {
	a := roleTestAuthz(t, testAllowModel, "p, alice, /reports, GET")
	roles, err := a.GetRolesForUser("alice")
	if err != nil {
		t.Fatal(err)
	}
	assertRoleSet(t, roles, nil)
}

func TestGetRolesForUserIsolatesDomains(t *testing.T) {
	a := roleTestAuthz(t, testDomainRoleModel, `g, alice, editor, tenant-1
g, alice, auditor, tenant-1
g, editor, admin, tenant-1
g, alice, viewer, tenant-2
g, bob, operator, tenant-2`)
	for _, tc := range []struct {
		name    string
		subject string
		domains []string
		want    []string
	}{
		{"first tenant", "alice", []string{"tenant-1"}, []string{"editor", "auditor"}},
		{"second tenant", "alice", []string{"tenant-2"}, []string{"viewer"}},
		{"role inheritance stays direct", "editor", []string{"tenant-1"}, []string{"admin"}},
		{"unknown tenant", "alice", []string{"tenant-3"}, nil},
		{"unknown subject", "unknown", []string{"tenant-1"}, nil},
		{"another user's role stays isolated", "bob", []string{"tenant-1"}, nil},
		{"domain omitted", "alice", nil, nil},
		{"empty domain", "alice", []string{""}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			roles, err := a.GetRolesForUser(tc.subject, tc.domains...)
			if err != nil {
				t.Fatal(err)
			}
			assertRoleSet(t, roles, tc.want)
		})
	}
}

// roleQueryManager overrides only the query boundary under test. The embedded
// manager supplies the remaining Casbin RoleManager operations.
type roleQueryManager struct {
	rbac.RoleManager
	getRoles func(string, ...string) ([]string, error)
}

func (m *roleQueryManager) GetRoles(name string, domain ...string) ([]string, error) {
	return m.getRoles(name, domain...)
}

func TestGetRolesForUserReturnsIndependentSnapshot(t *testing.T) {
	a := roleTestAuthz(t, testAllowModel, "g, alice, editor")
	stored := []string{"editor", "auditor"}
	a.engine.SetRoleManager(&roleQueryManager{
		RoleManager: a.engine.GetRoleManager(),
		getRoles: func(string, ...string) ([]string, error) {
			return stored, nil
		},
	})
	roles, err := a.GetRolesForUser("alice")
	if err != nil {
		t.Fatal(err)
	}
	assertRoleSet(t, roles, []string{"editor", "auditor"})
	roles[0] = "caller-edit"
	assertRoleSet(t, stored, []string{"editor", "auditor"})

	// Later role-manager changes must not alter a previously returned snapshot.
	stored[1] = "admin"
	if roles[1] != "auditor" {
		t.Fatalf("role-manager update changed the returned snapshot: %v", roles)
	}
	next, err := a.GetRolesForUser("alice")
	if err != nil {
		t.Fatal(err)
	}
	assertRoleSet(t, next, []string{"editor", "admin"})
	assertRoleSet(t, roles, []string{"caller-edit", "auditor"})
}

func TestGetRolesForUserPassesArgumentsAndErrors(t *testing.T) {
	failure := errors.New("role lookup unavailable")
	for _, tc := range []struct {
		name    string
		domains []string
	}{
		{"no domain", nil},
		{"empty domain", []string{""}},
		{"single domain", []string{"tenant-1"}},
		{"multiple arguments", []string{"tenant-1", "scope-2"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := roleTestAuthz(t, testAllowModel, "p, alice, /reports, GET")
			calls := 0
			a.engine.SetRoleManager(&roleQueryManager{
				RoleManager: a.engine.GetRoleManager(),
				getRoles: func(name string, gotDomains ...string) ([]string, error) {
					calls++
					if name != "alice" || !slices.Equal(gotDomains, tc.domains) {
						t.Errorf("query arguments=(%q, %v), want (alice, %v)", name, gotDomains, tc.domains)
					}
					return nil, failure
				},
			})
			roles, err := a.GetRolesForUser("alice", tc.domains...)
			if roles != nil || !errors.Is(err, failure) {
				t.Fatalf("roles=%v error=%v, want original lookup error", roles, err)
			}
			if calls != 1 {
				t.Fatalf("role queries=%d, want 1", calls)
			}
		})
	}
}

func TestGetRolesForUserWithoutRoleDefinition(t *testing.T) {
	text := strings.Replace(testAllowModel, "[role_definition]\ng = _, _\n", "", 1)
	text = strings.Replace(text, "g(r.sub, p.sub)", "r.sub == p.sub", 1)
	a := roleTestAuthz(t, text, "p, alice, /reports, GET")
	roles, err := a.GetRolesForUser("alice")
	if roles != nil || err == nil {
		t.Fatalf("model without role definition: roles=%v error=%v", roles, err)
	}
	lock := a.engine.GetLock()
	if !lock.TryLock() {
		t.Fatal("failed role query did not release the read lock")
	}
	lock.Unlock()
}

func TestGetRolesForUserHoldsReadLock(t *testing.T) {
	failure := errors.New("role lookup unavailable")
	for _, tc := range []struct {
		name string
		err  error
	}{{"success", nil}, {"error", failure}} {
		t.Run(tc.name, func(t *testing.T) {
			a := roleTestAuthz(t, testAllowModel, "p, alice, /reports, GET")
			entered, release := make(chan struct{}), make(chan struct{})
			var unblock sync.Once
			defer unblock.Do(func() { close(release) })
			a.engine.SetRoleManager(&roleQueryManager{
				RoleManager: a.engine.GetRoleManager(),
				getRoles: func(string, ...string) ([]string, error) {
					close(entered)
					<-release
					return nil, tc.err
				},
			})
			result := make(chan error, 1)
			go func() {
				_, err := a.GetRolesForUser("alice")
				result <- err
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("role query did not reach the role manager")
			}
			lock := a.engine.GetLock()
			// TryLock checks exclusion without depending on goroutine timing.
			if lock.TryLock() {
				lock.Unlock()
				t.Error("role query allowed an exclusive policy update during lookup")
			}
			if lock.TryRLock() {
				lock.RUnlock()
			} else {
				t.Error("role query blocked concurrent policy readers")
			}
			unblock.Do(func() { close(release) })
			if err := <-result; !errors.Is(err, tc.err) {
				t.Fatalf("query error=%v, want %v", err, tc.err)
			}
			if !lock.TryLock() {
				t.Fatal("role query did not release the read lock")
			}
			lock.Unlock()
		})
	}
}
