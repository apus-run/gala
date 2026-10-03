package authz

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/casbin/casbin/v3"
	"github.com/casbin/casbin/v3/model"
	"github.com/casbin/casbin/v3/persist"
	fileadapter "github.com/casbin/casbin/v3/persist/file-adapter"
	stringadapter "github.com/casbin/casbin/v3/persist/string-adapter"
	gormadapter "github.com/casbin/gorm-adapter/v3"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const testACLModel = `[request_definition]
r = sub, obj, act
[policy_definition]
p = sub, obj, act, eft
[role_definition]
g = _, _
[policy_effect]
e = !some(where (p.eft == deny))
[matchers]
m = g(r.sub, p.sub) && keyMatch(r.obj, p.obj) && r.act == p.act`

const testAllowModel = `[request_definition]
r = sub, obj, act
[policy_definition]
p = sub, obj, act
[role_definition]
g = _, _
[policy_effect]
e = some(where (p.eft == allow))
[matchers]
m = g(r.sub, p.sub) && keyMatch(r.obj, p.obj) && r.act == p.act`

func writeModelFile(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "model.conf")
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestNewAuthzIsolatesDatabaseSession(t *testing.T) {
	db := openPolicyDB(t)
	rule := gormadapter.CasbinRule{Ptype: "p", V0: "alice", V1: "/reports", V2: "GET"}
	if err := db.Create(&rule).Error; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	requestDB := db.WithContext(ctx).Where("v0 = ?", "nobody")
	a, err := NewAuthz(requestDB,
		WithModelFilePath(writeModelFile(t, testAllowModel)),
	)
	if err != nil {
		t.Fatal(err)
	}
	if allowed, err := a.Authorize("alice", "/reports", "GET"); err != nil || !allowed {
		t.Fatalf("caller conditions leaked into initial load: allowed=%v error=%v", allowed, err)
	}
	if err := a.LoadPolicy(); err != nil {
		t.Fatal(err)
	}
	if allowed, err := a.Authorize("alice", "/reports", "GET"); err != nil || !allowed {
		t.Fatalf("caller conditions leaked into policy queries: allowed=%v error=%v", allowed, err)
	}
	if requestDB.Statement.Context != ctx || ctx.Err() != context.Canceled {
		t.Fatal("caller context was changed")
	}
	if err := requestDB.Find(&[]gormadapter.CasbinRule{}).Error; !errors.Is(err, context.Canceled) {
		t.Fatalf("caller session was changed: %v", err)
	}
}

func roleTestAuthz(t *testing.T, text, policy string) *Authorizer {
	t.Helper()
	m, err := model.NewModelFromString(text)
	if err != nil {
		t.Fatal(err)
	}
	e, err := casbin.NewSyncedEnforcer(m, stringadapter.NewAdapter(policy))
	if err != nil {
		t.Fatal(err)
	}
	return &Authorizer{engine: e}
}

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "policy.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func openPolicyDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := openTestDB(t)
	if err := db.AutoMigrate(&gormadapter.CasbinRule{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestNewAppliesOptionsInOrder(t *testing.T) {
	db := openTestDB(t)
	adapter, err := gormadapter.NewAdapterByDB(db)
	if err != nil {
		t.Fatal(err)
	}
	policyReads := 0
	if err := db.Callback().Query().After("gorm:query").Register("authz:test-policy-reads", func(tx *gorm.DB) {
		if tx.Statement.Table == "casbin_rule" {
			policyReads++
		}
	}); err != nil {
		t.Fatal(err)
	}
	denyByDefault := strings.Replace(testACLModel, "!some(where (p.eft == deny))", "some(where (p.eft == allow)) && !some(where (p.eft == deny))", 1)
	a, err := New(
		WithModelFilePath(writeModelFile(t, testACLModel)),
		WithPolicyAdapter(adapter),
		WithModelFilePath(writeModelFile(t, denyByDefault)),
	)
	if err != nil {
		t.Fatal(err)
	}
	if policyReads != 1 {
		t.Fatalf("initial policy reads=%d, want 1", policyReads)
	}
	if a.engine.IsAutoLoadingRunning() {
		t.Fatal("constructor started a background worker")
	}
	allowed, err := a.Authorize("alice", "/reports", "GET")
	if err != nil || allowed {
		t.Fatalf("empty policy decision=%v error=%v", allowed, err)
	}
	if _, err := a.engine.AddPolicy("reader", "/reports", "GET", "allow"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.engine.AddGroupingPolicy("alice", "reader"); err != nil {
		t.Fatal(err)
	}
	allowed, err = a.Authorize("alice", "/reports", "GET")
	if err != nil || !allowed {
		t.Fatalf("role policy decision=%v error=%v", allowed, err)
	}
}

type countingAdapter struct {
	persist.Adapter
	calls int
	err   error
}

func (a *countingAdapter) LoadPolicy(model.Model) error {
	a.calls++
	return a.err
}

func TestNewRejectsInvalidInputsBeforePolicyLoading(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts []Option
	}{
		{"empty model", []Option{WithModelFilePath("")}},
		{"invalid model", []Option{WithModelFilePath(writeModelFile(t, "invalid model"))}},
		{"missing model", []Option{WithModelFilePath(filepath.Join(t.TempDir(), "missing.conf"))}},
		{"zero timeout", []Option{WithPolicyLoadTimeout(0)}},
		{"negative timeout", []Option{WithPolicyLoadTimeout(-time.Second)}},
		{"nil adapter", []Option{WithPolicyAdapter(nil)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			adapter := &countingAdapter{}
			opts := slices.Concat([]Option{WithModelFilePath(writeModelFile(t, testACLModel)), WithPolicyAdapter(adapter)}, tc.opts)
			if a, err := New(opts...); a != nil || err == nil {
				t.Fatalf("authorizer=%v error=%v", a, err)
			}
			if adapter.calls != 0 {
				t.Fatal("invalid inputs accessed policy storage")
			}
		})
	}
}

func TestNewAuthzInitialLoadUsesPolicyTimeout(t *testing.T) {
	db := openPolicyDB(t)
	if err := db.Callback().Query().Before("gorm:query").Register("authz:test-initial-timeout", func(tx *gorm.DB) {
		if tx.Statement.Table != "casbin_rule" {
			return
		}
		select {
		case <-tx.Statement.Context.Done():
			tx.AddError(tx.Statement.Context.Err())
		case <-time.After(time.Second):
			tx.AddError(errors.New("policy query had no deadline"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	a, err := NewAuthz(db,
		WithModelFilePath(writeModelFile(t, testACLModel)),
		WithPolicyLoadTimeout(20*time.Millisecond),
	)
	if a != nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("initial policy load timeout: authorizer=%v error=%v", a, err)
	}
}

func TestNewDefaultFiles(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("model.conf", []byte(testACLModel), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("policy.csv", []byte("g, alice, reader\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, opts := range [][]Option{nil, {WithModelFilePath("./model.conf"), WithPolicyAdapter(fileadapter.NewAdapter("./policy.csv"))}} {
		a, err := New(opts...)
		if err != nil {
			t.Fatal(err)
		}
		roles, err := a.GetRolesForUser("alice")
		if err != nil || len(roles) != 1 || roles[0] != "reader" {
			t.Fatal(roles, err)
		}
	}
}

func TestNewPropagatesStorageErrors(t *testing.T) {
	failure := errors.New("policy store unavailable")
	adapter := &countingAdapter{err: failure}
	if a, err := New(WithModelFilePath(writeModelFile(t, testACLModel)), WithPolicyAdapter(adapter)); a != nil || !errors.Is(err, failure) {
		t.Fatalf("authorizer=%v error=%v", a, err)
	}
	if adapter.calls != 1 {
		t.Fatalf("policy loads=%d want=1", adapter.calls)
	}
	missing := filepath.Join(t.TempDir(), "missing.csv")
	if a, err := New(WithModelFilePath(writeModelFile(t, testACLModel)), WithPolicyAdapter(fileadapter.NewAdapter(missing))); a != nil || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("authorizer=%v error=%v", a, err)
	}
}

func TestNewAuthzUsesGORMStorage(t *testing.T) {
	db := openPolicyDB(t)
	unused := &countingAdapter{}
	a, err := NewAuthz(db,
		WithModelFilePath(writeModelFile(t, testACLModel)),
		WithPolicyAdapter(unused),
	)
	if err != nil {
		t.Fatal(err)
	}
	if unused.calls != 0 {
		t.Fatal("NewAuthz used the configured adapter instead of db")
	}
	if _, err := a.engine.AddGroupingPolicy("alice", "reader"); err != nil {
		t.Fatal(err)
	}
	var stored int64
	if err := db.Model(&gormadapter.CasbinRule{}).Count(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored != 0 {
		t.Fatal("authorizer automatically persisted an in-memory policy change")
	}
	rule := gormadapter.CasbinRule{Ptype: "g", V0: "alice", V1: "reader"}
	if err := db.Create(&rule).Error; err != nil {
		t.Fatal(err)
	}
	if err := a.LoadPolicy(); err != nil {
		t.Fatal(err)
	}
	roles, err := a.GetRolesForUser("alice")
	if err != nil || len(roles) != 1 || roles[0] != "reader" {
		t.Fatal(roles, err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Ping(); err != nil {
		t.Fatal("caller database was closed", err)
	}
}

func TestNewAuthzRejectsInvalidInputsBeforeStorageAccess(t *testing.T) {
	if a, err := NewAuthz(nil, OptionFunc(func(*Options) {
		t.Error("nil database should be rejected before applying options")
	})); a != nil || err == nil {
		t.Fatalf("nil database: authorizer=%v error=%v", a, err)
	}
	for _, opts := range [][]Option{
		{WithModelFilePath(writeModelFile(t, "invalid model"))},
		{WithPolicyLoadTimeout(0)},
		{WithPolicyAdapter(nil)},
	} {
		db := openTestDB(t)
		reads := 0
		if err := db.Callback().Query().Before("gorm:query").Register("authz:test-invalid-input", func(*gorm.DB) {
			reads++
		}); err != nil {
			t.Fatal(err)
		}
		if a, err := NewAuthz(db, opts...); a != nil || err == nil {
			t.Fatalf("invalid inputs: authorizer=%v error=%v", a, err)
		}
		if reads != 0 {
			t.Fatal("invalid inputs accessed policy storage")
		}
		if db.Migrator().HasTable("casbin_rule") {
			t.Fatal("invalid inputs created policy table")
		}
	}
}

func TestNewDoesNotRetainMutableOptions(t *testing.T) {
	adapter := stringadapter.NewAdapter("g, alice, reader")
	var captured *Options
	a, err := New(
		WithModelFilePath(writeModelFile(t, testACLModel)),
		WithPolicyAdapter(adapter),
		OptionFunc(func(o *Options) { captured = o }),
	)
	if err != nil {
		t.Fatal(err)
	}
	captured.Apply(WithModelFilePath("missing.conf"), WithPolicyAdapter(stringadapter.NewAdapter("g, alice, admin")))
	if err := a.LoadPolicy(); err != nil {
		t.Fatal("retained options changed reload configuration", err)
	}
	roles, err := a.GetRolesForUser("alice")
	if err != nil || len(roles) != 1 || roles[0] != "reader" {
		t.Fatal("option mutations changed the constructed authorizer", roles, err)
	}
}

func TestAuthorizeDecisions(t *testing.T) {
	a, err := New(
		WithModelFilePath(writeModelFile(t, testAllowModel)),
		WithPolicyAdapter(stringadapter.NewAdapter("p, reader, /reports/*, GET\ng, alice, reader")),
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, sub, obj, act string
		allowed             bool
	}{
		{"inherited permission", "alice", "/reports/1", "GET", true},
		{"role itself", "reader", "/reports/1", "GET", true},
		{"unknown subject", "bob", "/reports/1", "GET", false},
		{"different object", "alice", "/settings", "GET", false},
		{"different action", "alice", "/reports/1", "DELETE", false},
		{"empty request", "", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			allowed, err := a.Authorize(tc.sub, tc.obj, tc.act)
			if err != nil || allowed != tc.allowed {
				t.Fatalf("allowed=%v error=%v, want %v", allowed, err, tc.allowed)
			}
		})
	}
}

func TestAuthorizeDenyOverridesAllow(t *testing.T) {
	text := strings.Replace(testACLModel, "!some(where (p.eft == deny))", "some(where (p.eft == allow)) && !some(where (p.eft == deny))", 1)
	a, err := New(
		WithModelFilePath(writeModelFile(t, text)),
		WithPolicyAdapter(stringadapter.NewAdapter("p, reader, /reports/*, GET, allow\np, reader, /reports/private/*, GET, deny\ng, alice, reader")),
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		object  string
		allowed bool
	}{{"/reports/1", true}, {"/reports/private/1", false}, {"/settings", false}} {
		allowed, err := a.Authorize("alice", tc.object, "GET")
		if err != nil || allowed != tc.allowed {
			t.Fatalf("object=%s allowed=%v error=%v, want %v", tc.object, allowed, err, tc.allowed)
		}
	}
}

func TestEnforceModelSpecificValues(t *testing.T) {
	const domainModel = `[request_definition]
r = sub, dom, obj, act
[policy_definition]
p = sub, dom, obj, act
[policy_effect]
e = some(where (p.eft == allow))
[matchers]
m = r.sub == p.sub && r.dom == p.dom && r.obj == p.obj && r.act == p.act`
	a, err := New(
		WithModelFilePath(writeModelFile(t, domainModel)),
		WithPolicyAdapter(stringadapter.NewAdapter("p, alice, tenant-1, /reports, GET")),
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		domain  string
		allowed bool
	}{{"tenant-1", true}, {"tenant-2", false}} {
		allowed, err := a.Enforce("alice", tc.domain, "/reports", "GET")
		if err != nil || allowed != tc.allowed {
			t.Fatalf("domain=%s allowed=%v error=%v, want %v", tc.domain, allowed, err, tc.allowed)
		}
	}
	for _, values := range [][]any{nil, {"alice", "/reports", "GET"}, {"alice", "tenant-1", "/reports", "GET", "extra"}} {
		allowed, err := a.Enforce(values...)
		if allowed || err == nil || !strings.HasPrefix(err.Error(), "authz: enforce: ") {
			t.Fatalf("values=%v allowed=%v error=%v", values, allowed, err)
		}
	}
	if allowed, err := a.Authorize("alice", "/reports", "GET"); allowed || err == nil {
		t.Fatalf("three-field convenience method accepted four-field model: allowed=%v error=%v", allowed, err)
	}
}

func TestEnforcePreservesErrorCause(t *testing.T) {
	text := strings.Replace(testAllowModel, "g(r.sub, p.sub)", "checkSubject(r.sub, p.sub)", 1)
	a, err := New(
		WithModelFilePath(writeModelFile(t, text)),
		WithPolicyAdapter(stringadapter.NewAdapter("p, alice, /reports, GET")),
	)
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("subject lookup unavailable")
	a.engine.AddFunction("checkSubject", func(...any) (any, error) { return nil, failure })
	allowed, err := a.Authorize("alice", "/reports", "GET")
	if allowed || !errors.Is(err, failure) || !strings.HasPrefix(err.Error(), "authz: enforce: ") {
		t.Fatalf("allowed=%v error=%v", allowed, err)
	}
}

func TestNewAuthzRequiresExistingPolicyTable(t *testing.T) {
	db := openTestDB(t)
	a, err := NewAuthz(db, WithModelFilePath(writeModelFile(t, testAllowModel)))
	if a != nil || err == nil || !strings.HasPrefix(err.Error(), "authz: initial policy load: ") {
		t.Fatalf("authorizer=%v error=%v", a, err)
	}
	if db.Migrator().HasTable("casbin_rule") {
		t.Fatal("NewAuthz migrated the policy table")
	}
	// Disabling migration for the authorizer must not disable it on the caller's DB.
	if _, err := gormadapter.NewAdapterByDB(db); err != nil {
		t.Fatal(err)
	}
	if !db.Migrator().HasTable("casbin_rule") {
		t.Fatal("authorizer changed the caller's migration configuration")
	}
}

type filteredCountingAdapter struct {
	countingAdapter
	filtered bool
}

func (a *filteredCountingAdapter) IsFiltered() bool { return a.filtered }

func (a *filteredCountingAdapter) LoadFilteredPolicy(model.Model, any) error {
	return errors.New("unexpected filtered policy load")
}

func TestNewRejectsAlreadyFilteredAdapter(t *testing.T) {
	adapter := &filteredCountingAdapter{filtered: true}
	a, err := New(WithModelFilePath(writeModelFile(t, testACLModel)), WithPolicyAdapter(adapter))
	if a != nil || err == nil {
		t.Fatalf("filtered adapter silently skipped initial policy load: authorizer=%v error=%v", a, err)
	}
	if adapter.calls != 0 {
		t.Fatal("constructor loaded all policies from an already filtered adapter")
	}
}

func TestNewLoadsUnfilteredFilterCapableAdapter(t *testing.T) {
	adapter := &filteredCountingAdapter{}
	if _, err := New(WithModelFilePath(writeModelFile(t, testAllowModel)), WithPolicyAdapter(adapter)); err != nil {
		t.Fatal(err)
	}
	if adapter.calls != 1 {
		t.Fatalf("initial policy loads=%d, want 1", adapter.calls)
	}
}
