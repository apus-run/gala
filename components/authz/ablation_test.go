package authz

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/casbin/casbin/v3"
	"github.com/casbin/casbin/v3/model"
	"github.com/casbin/casbin/v3/persist"
)

// This ablates whole-load serialization, not a fabricated snapshot-application
// schedule. A blocked adapter still holds Casbin's RLock, so neither concurrent
// load can apply until that read lock is released.
func TestAblationWholeLoadSerialization(t *testing.T) {
	for _, serialized := range []bool{false, true} {
		t.Run(map[bool]string{false: "without_outer_lock", true: "with_outer_lock"}[serialized], func(t *testing.T) {
			first, second, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var count atomic.Int32
			adapter := &reloadAdapter{load: func(m model.Model) error {
				n := count.Add(1)
				if n == 2 {
					close(first)
					<-release
				}
				if n == 3 {
					close(second)
				}
				return persist.LoadPolicyLine(alicePolicy, m)
			}}
			a := newSyncAuthorizer(t, adapter)
			load := a.engine.LoadPolicy
			if serialized {
				load = a.LoadPolicy
			}
			done := make(chan error, 2)
			go func() { done <- load() }()
			<-first
			go func() { done <- load() }()
			if serialized {
				select {
				case <-second:
					t.Error("adapter loads overlapped")
				case <-time.After(20 * time.Millisecond):
				}
			} else {
				select {
				case <-second:
				case <-time.After(time.Second):
					t.Error("expected overlapping adapter reads")
				}
			}
			close(release)
			for range 2 {
				if err := <-done; err != nil {
					t.Error(err)
				}
			}
		})
	}
}

func TestAblationWatcherExNeedsExplicitCallback(t *testing.T) {
	s := &syncSource{policy: alicePolicy}
	m, err := model.NewModelFromString(testAllowModel)
	if err != nil {
		t.Fatal(err)
	}
	e, err := casbin.NewSyncedEnforcer(m, s)
	if err != nil {
		t.Fatal(err)
	}
	w := &testWatcherEx{}
	if err := e.SetWatcher(w); err != nil {
		t.Fatal(err)
	}
	if w.callback != nil {
		t.Fatal("upstream WatcherEx behavior changed")
	}
	newSyncAuthorizer(t, s, WithWatcher(w), WithWatcherErrorHandler(func(err error) { t.Error(err) }))
	if w.callback == nil {
		t.Fatal("explicit registration missing")
	}
}

// Both variants install the same watcher and revoke at the same registration
// point. Only the post-registration reload differs. Production constructor
// ordering is separately checked by TestWatcherCatchUpClosesStartupGap.
func TestAblationStartupCatchUp(t *testing.T) {
	for _, catchUp := range []bool{false, true} {
		t.Run(map[bool]string{false: "without_catch_up", true: "with_catch_up"}[catchUp], func(t *testing.T) {
			source := &syncSource{policy: alicePolicy}
			a := newSyncAuthorizer(t, source)
			watcher := &testWatcher{register: func() { source.set("", nil) }}
			if err := watcher.SetUpdateCallback(func(string) {
				if err := a.LoadPolicy(); err != nil {
					t.Error(err)
				}
			}); err != nil {
				t.Fatal(err)
			}
			if catchUp {
				if err := a.LoadPolicy(); err != nil {
					t.Fatal(err)
				}
			}
			assertAlice(t, a, !catchUp)
		})
	}
}

// Only WithMaxPolicyAge differs. Use elapsed time and the public Enforce API;
// failed reloads must not renew the successful snapshot's age.
func TestAblationPolicyExpiry(t *testing.T) {
	const age = 100 * time.Millisecond
	for _, expiry := range []bool{false, true} {
		t.Run(map[bool]string{false: "without_expiry", true: "with_expiry"}[expiry], func(t *testing.T) {
			source := &syncSource{policy: alicePolicy}
			limit := time.Duration(0)
			if expiry {
				limit = age
			}
			a := newSyncAuthorizer(t, source, WithMaxPolicyAge(limit))
			assertAlice(t, a, true)
			source.set("", errors.New("store unavailable"))
			if err := a.LoadPolicy(); err == nil {
				t.Fatal("expected failed reload")
			}
			time.Sleep(age + 10*time.Millisecond)
			allowed, err := a.Enforce("alice", "/reports", "GET")
			if expiry {
				if allowed || !errors.Is(err, ErrPolicyStale) {
					t.Fatalf("allowed=%v err=%v", allowed, err)
				}
			} else if !allowed || err != nil {
				t.Fatalf("allowed=%v err=%v", allowed, err)
			}
		})
	}
}
