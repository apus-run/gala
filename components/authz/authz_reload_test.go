package authz

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/casbin/casbin/v3/model"
	"github.com/casbin/casbin/v3/persist"
	fileadapter "github.com/casbin/casbin/v3/persist/file-adapter"
	stringadapter "github.com/casbin/casbin/v3/persist/string-adapter"
	gormadapter "github.com/casbin/gorm-adapter/v3"
	"gorm.io/gorm"
)

type reloadAdapter struct {
	persist.Adapter
	load func(model.Model) error
}

func (a *reloadAdapter) LoadPolicy(m model.Model) error { return a.load(m) }

func TestLoadPolicyReloadsFileWithoutReloadingModel(t *testing.T) {
	modelPath := writeModelFile(t, testAllowModel)
	policyPath := filepath.Join(t.TempDir(), "policy.csv")
	if err := os.WriteFile(policyPath, []byte("p, alice, /reports, GET\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a, err := New(WithModelFilePath(modelPath), WithPolicyAdapter(fileadapter.NewAdapter(policyPath)))
	if err != nil {
		t.Fatal(err)
	}
	if allowed, err := a.Authorize("alice", "/reports", "GET"); err != nil || !allowed {
		t.Fatalf("initial file policy: allowed=%v error=%v", allowed, err)
	}
	if err := os.Remove(modelPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(policyPath, []byte("p, bob, /reports, GET\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.LoadPolicy(); err != nil {
		t.Fatal("policy reload should reuse the existing model", err)
	}
	if allowed, err := a.Authorize("alice", "/reports", "GET"); err != nil || allowed {
		t.Fatalf("reload retained revoked file permission: allowed=%v error=%v", allowed, err)
	}
	if allowed, err := a.Authorize("bob", "/reports", "GET"); err != nil || !allowed {
		t.Fatalf("reload did not apply new file permission: allowed=%v error=%v", allowed, err)
	}
	if err := os.Remove(policyPath); err != nil {
		t.Fatal(err)
	}
	if err := a.LoadPolicy(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing policy file error=%v", err)
	}
	if allowed, err := a.Authorize("bob", "/reports", "GET"); err != nil || !allowed {
		t.Fatalf("missing file lost the last loaded policy: allowed=%v error=%v", allowed, err)
	}
}

func TestLoadPolicyFailureKeepsLastPolicyAndCanRetry(t *testing.T) {
	policy := "p, reader, /reports, GET\ng, alice, reader"
	var failure error
	calls := 0
	adapter := &reloadAdapter{load: func(m model.Model) error {
		calls++
		if err := stringadapter.NewAdapter(policy).LoadPolicy(m); err != nil {
			return err
		}
		// A storage error can occur after the adapter has partially filled the model.
		return failure
	}}
	a, err := New(WithModelFilePath(writeModelFile(t, testAllowModel)), WithPolicyAdapter(adapter))
	if err != nil {
		t.Fatal(err)
	}
	check := func(alice, bob bool) {
		t.Helper()
		for _, tc := range []struct {
			sub     string
			allowed bool
		}{{"alice", alice}, {"bob", bob}} {
			allowed, err := a.Authorize(tc.sub, "/reports", "GET")
			if err != nil || allowed != tc.allowed {
				t.Fatalf("subject=%s allowed=%v error=%v, want %v", tc.sub, allowed, err, tc.allowed)
			}
		}
	}
	check(true, false)
	if calls != 1 {
		t.Fatalf("authorization accessed policy storage: loads=%d, want 1", calls)
	}
	policy = "p, writer, /reports, GET\ng, bob, writer"
	failure = errors.New("policy store unavailable")
	if err := a.LoadPolicy(); !errors.Is(err, failure) || !strings.HasPrefix(err.Error(), "authz: reload policy: ") {
		t.Fatalf("reload error=%v", err)
	}
	check(true, false)
	roles, err := a.GetRolesForUser("alice")
	if err != nil || len(roles) != 1 || roles[0] != "reader" {
		t.Fatalf("failed reload changed roles: roles=%v error=%v", roles, err)
	}
	failure = nil
	if err := a.LoadPolicy(); err != nil {
		t.Fatal(err)
	}
	check(false, true)
	roles, err = a.GetRolesForUser("alice")
	if err != nil || len(roles) != 0 {
		t.Fatalf("successful reload retained revoked roles: roles=%v error=%v", roles, err)
	}
	roles, err = a.GetRolesForUser("bob")
	if err != nil || len(roles) != 1 || roles[0] != "writer" {
		t.Fatalf("successful reload did not apply new roles: roles=%v error=%v", roles, err)
	}
	if calls != 3 {
		t.Fatalf("policy loads=%d, want 3", calls)
	}
}

func TestLoadPolicySerializesConcurrentReloads(t *testing.T) {
	const workers, reloads = 8, 8
	var active, maximum, calls atomic.Int32
	adapter := &reloadAdapter{load: func(m model.Model) error {
		calls.Add(1)
		n := active.Add(1)
		defer active.Add(-1)
		for old := maximum.Load(); n > old; old = maximum.Load() {
			if maximum.CompareAndSwap(old, n) {
				break
			}
		}
		// Yield during the storage read so competing reloads can attempt to enter.
		time.Sleep(time.Millisecond)
		return stringadapter.NewAdapter("p, reader, /reports, GET\ng, alice, reader").LoadPolicy(m)
	}}
	a, err := New(WithModelFilePath(writeModelFile(t, testAllowModel)), WithPolicyAdapter(adapter))
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, workers)
	for range workers {
		go func() {
			<-start
			for range reloads {
				if err := a.LoadPolicy(); err != nil {
					results <- err
					return
				}
				if allowed, err := a.Authorize("alice", "/reports", "GET"); err != nil || !allowed {
					results <- errors.New("authorization lost the current policy during reload")
					return
				}
				if roles, err := a.GetRolesForUser("alice"); err != nil || len(roles) != 1 || roles[0] != "reader" {
					results <- errors.New("role query lost the current policy during reload")
					return
				}
			}
			results <- nil
		}()
	}
	close(start)
	for range workers {
		if err := <-results; err != nil {
			t.Error(err)
		}
	}
	if maximum.Load() != 1 {
		t.Fatalf("concurrent adapter loads=%d, want 1", maximum.Load())
	}
	if calls.Load() != 1+workers*reloads {
		t.Fatalf("policy loads=%d, want %d", calls.Load(), 1+workers*reloads)
	}
}

func TestLoadPolicyAllowsReadsDuringStorageLoad(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var unblock sync.Once
	defer unblock.Do(func() { close(release) })
	block := false
	adapter := &reloadAdapter{load: func(m model.Model) error {
		if block {
			close(entered)
			<-release
		}
		return stringadapter.NewAdapter("p, reader, /reports, GET\ng, alice, reader").LoadPolicy(m)
	}}
	a, err := New(WithModelFilePath(writeModelFile(t, testAllowModel)), WithPolicyAdapter(adapter))
	if err != nil {
		t.Fatal(err)
	}
	block = true
	loaded := make(chan error, 1)
	go func() { loaded <- a.LoadPolicy() }()
	select {
	case <-entered:
	case err := <-loaded:
		t.Fatalf("reload did not access storage: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("reload did not reach storage")
	}
	read := make(chan error, 1)
	go func() {
		if allowed, err := a.Authorize("alice", "/reports", "GET"); err != nil || !allowed {
			read <- errors.New("authorization failed while policy storage was blocked")
			return
		}
		if roles, err := a.GetRolesForUser("alice"); err != nil || len(roles) != 1 || roles[0] != "reader" {
			read <- errors.New("role query failed while policy storage was blocked")
			return
		}
		read <- nil
	}()
	timedOut := false
	select {
	case err := <-read:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(5 * time.Second):
		timedOut = true
		t.Error("policy storage blocked reads of the existing in-memory policy")
	}
	unblock.Do(func() { close(release) })
	if err := <-loaded; err != nil {
		t.Fatal(err)
	}
	if timedOut {
		<-read
	}
}

func TestNewAuthzReloadUsesFreshTimeout(t *testing.T) {
	db := openPolicyDB(t)
	rule := gormadapter.CasbinRule{Ptype: "p", V0: "alice", V1: "/reports", V2: "GET"}
	if err := db.Create(&rule).Error; err != nil {
		t.Fatal(err)
	}
	var contexts []context.Context
	if err := db.Callback().Query().Before("gorm:query").Register("authz:test-reload-timeout", func(tx *gorm.DB) {
		if tx.Statement.Table != "casbin_rule" {
			return
		}
		ctx := tx.Statement.Context
		contexts = append(contexts, ctx)
		if _, ok := ctx.Deadline(); !ok {
			tx.AddError(errors.New("policy query had no deadline"))
			return
		}
		if len(contexts) == 2 {
			<-ctx.Done()
			tx.AddError(ctx.Err())
		}
	}); err != nil {
		t.Fatal(err)
	}
	a, err := NewAuthz(db,
		WithModelFilePath(writeModelFile(t, testAllowModel)),
		WithPolicyLoadTimeout(100*time.Millisecond),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&rule).Error; err != nil {
		t.Fatal(err)
	}
	if err := a.LoadPolicy(); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("reload timeout error=%v", err)
	}
	if allowed, err := a.Authorize("alice", "/reports", "GET"); err != nil || !allowed {
		t.Fatalf("timed-out reload lost the old policy: allowed=%v error=%v", allowed, err)
	}
	if err := a.LoadPolicy(); err != nil {
		t.Fatal("a previous timeout poisoned the next reload", err)
	}
	if allowed, err := a.Authorize("alice", "/reports", "GET"); err != nil || allowed {
		t.Fatalf("successful reload retained revoked permission: allowed=%v error=%v", allowed, err)
	}
	if len(contexts) != 3 {
		t.Fatalf("policy reads=%d, want 3", len(contexts))
	}
	for i, ctx := range contexts {
		if ctx.Err() == nil {
			t.Fatalf("policy read %d did not release its context", i)
		}
		for _, previous := range contexts[:i] {
			if ctx == previous {
				t.Fatalf("policy read %d reused an earlier context", i)
			}
		}
	}
}
