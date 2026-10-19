package authz

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/casbin/casbin/v3/model"
	"github.com/casbin/casbin/v3/persist"
	stringadapter "github.com/casbin/casbin/v3/persist/string-adapter"
)

type testWatcher struct {
	persist.Watcher
	callback        func(string)
	register        func()
	registrationErr error
	update          func() error
}

func (w *testWatcher) SetUpdateCallback(cb func(string)) error {
	if w.register != nil {
		w.register()
	}
	w.callback = cb
	return w.registrationErr
}
func (w *testWatcher) Update() error {
	if w.update != nil {
		return w.update()
	}
	w.callback("")
	return nil
}
func (w *testWatcher) Close() {}

// This is the branch Casbin's SetWatcher does not install a callback for.
type testWatcherEx struct{ testWatcher }

func (w *testWatcherEx) UpdateForAddPolicy(string, string, ...string) error    { return nil }
func (w *testWatcherEx) UpdateForRemovePolicy(string, string, ...string) error { return nil }
func (w *testWatcherEx) UpdateForRemoveFilteredPolicy(string, string, int, ...string) error {
	return nil
}
func (w *testWatcherEx) UpdateForSavePolicy(model.Model) error                     { return nil }
func (w *testWatcherEx) UpdateForAddPolicies(string, string, ...[]string) error    { return nil }
func (w *testWatcherEx) UpdateForRemovePolicies(string, string, ...[]string) error { return nil }

var _ persist.WatcherEx = (*testWatcherEx)(nil)

type syncSource struct {
	persist.Adapter
	mu      sync.Mutex
	policy  string
	failure error
}

func (s *syncSource) set(policy string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.policy = policy
	s.failure = err
}
func (s *syncSource) LoadPolicy(m model.Model) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failure != nil {
		return s.failure
	}
	if s.policy == "" {
		return nil
	}
	return stringadapter.NewAdapter(s.policy).LoadPolicy(m)
}
func newSyncAuthorizer(t *testing.T, s persist.Adapter, opts ...Option) *Authorizer {
	t.Helper()
	opts = append([]Option{WithModelFilePath(writeModelFile(t, testAllowModel)), WithPolicyAdapter(s)}, opts...)
	a, err := New(opts...)
	if err != nil {
		t.Fatal(err)
	}
	return a
}
func assertAlice(t *testing.T, a *Authorizer, want bool) {
	t.Helper()
	got, err := a.Enforce("alice", "/reports", "GET")
	if err != nil || got != want {
		t.Fatalf("allowed=%v err=%v want=%v", got, err, want)
	}
}

const alicePolicy = "p, alice, /reports, GET"

func TestWatcherCatchUpClosesStartupGap(t *testing.T) {
	for _, ex := range []bool{false, true} {
		t.Run(map[bool]string{false: "Watcher", true: "WatcherEx"}[ex], func(t *testing.T) {
			s := &syncSource{policy: alicePolicy}
			w := &testWatcher{register: func() { s.set("", nil) }}
			var injected persist.Watcher = w
			if ex {
				x := &testWatcherEx{testWatcher: *w}
				w = &x.testWatcher
				injected = x
			}
			a := newSyncAuthorizer(t, s, WithWatcher(injected), WithWatcherErrorHandler(func(e error) { t.Error(e) }))
			assertAlice(t, a, false)
			s.set(alicePolicy, nil)
			w.callback("ignored payload")
			assertAlice(t, a, true)
		})
	}
}
func TestSyncPolicyNotifiesDespiteLocalFailureAndPreservesBothErrors(t *testing.T) {
	localErr, notifyErr := errors.New("local"), errors.New("notify")
	local, remote := &syncSource{policy: alicePolicy}, &syncSource{policy: alicePolicy}
	peer := newSyncAuthorizer(t, remote)
	calls := 0
	w := &testWatcher{update: func() error {
		calls++
		if err := peer.LoadPolicy(); err != nil {
			t.Fatal(err)
		}
		return notifyErr
	}}
	a := newSyncAuthorizer(t, local, WithWatcher(w), WithWatcherErrorHandler(func(e error) { t.Error(e) }))
	local.set("", localErr)
	remote.set("", nil)
	err := a.SyncPolicy()
	if !errors.Is(err, localErr) || !errors.Is(err, notifyErr) || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
	assertAlice(t, peer, false)
	assertAlice(t, a, true)
}
func TestSyncPolicyAllowsSynchronousEcho(t *testing.T) {
	s := &syncSource{policy: alicePolicy}
	w := &testWatcher{}
	a := newSyncAuthorizer(t, s, WithWatcher(w), WithWatcherErrorHandler(func(e error) { t.Error(e) }))
	done := make(chan error, 1)
	go func() { done <- a.SyncPolicy() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("synchronous callback deadlocked")
	}
}
func TestWatcherCallbackReportsFailureAndKeepsPolicy(t *testing.T) {
	s := &syncSource{policy: alicePolicy}
	w := &testWatcher{}
	var reported error
	a := newSyncAuthorizer(t, s, WithWatcher(w), WithWatcherErrorHandler(func(e error) { reported = e }))
	failure := errors.New("storage")
	s.set("", failure)
	w.callback("")
	if !errors.Is(reported, failure) {
		t.Fatal(reported)
	}
	assertAlice(t, a, true)
}
func TestWatcherValidationAndRegistrationFailure(t *testing.T) {
	var nilWatcher *testWatcher
	for _, opts := range [][]Option{{WithWatcher(nilWatcher)}, {WithWatcher(&testWatcher{})}, {WithWatcherErrorHandler(func(error) {})}, {WithMaxPolicyAge(-1)}} {
		if err := NewOptions(opts...).Validate(); err == nil {
			t.Fatal("expected validation error")
		}
	}
	s := &syncSource{policy: alicePolicy}
	failure := errors.New("register")
	a, err := New(WithModelFilePath(writeModelFile(t, testAllowModel)), WithPolicyAdapter(s), WithWatcher(&testWatcher{registrationErr: failure}), WithWatcherErrorHandler(func(error) {}))
	if a != nil || !errors.Is(err, failure) {
		t.Fatalf("a=%v err=%v", a, err)
	}
}
func TestExpiredPolicyFailsClosedUntilSuccessfulReload(t *testing.T) {
	s := &syncSource{policy: alicePolicy}
	a := newSyncAuthorizer(t, s, WithMaxPolicyAge(time.Second))
	old := time.Now().Add(-2 * time.Second)
	a.lastLoad.Store(&old)
	if ok, err := a.Enforce("alice", "/reports", "GET"); ok || !errors.Is(err, ErrPolicyStale) {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if _, err := a.GetRolesForUser("alice"); !errors.Is(err, ErrPolicyStale) {
		t.Fatal(err)
	}
	s.set("", errors.New("storage"))
	_ = a.LoadPolicy()
	if !errors.Is(a.policyFresh(), ErrPolicyStale) {
		t.Fatal("failure refreshed age")
	}
	s.set("", nil)
	if err := a.LoadPolicy(); err != nil {
		t.Fatal(err)
	}
	assertAlice(t, a, false)
}
func TestRunPolicyReloadRepairsMissedSignalAndStops(t *testing.T) {
	s := &syncSource{policy: alicePolicy}
	a := newSyncAuthorizer(t, s, WithMaxPolicyAge(time.Second))
	s.set("", nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- a.RunPolicyReload(ctx, time.Millisecond, func(err error) { t.Error(err) }) }()
	deadline := time.After(time.Second)
	for {
		ok, err := a.Enforce("alice", "/reports", "GET")
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		select {
		case <-deadline:
			t.Fatal("missed event not repaired")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("loop did not stop")
	}
}

func TestPeriodicReloadRecoversAfterErrorWithoutNewEvent(t *testing.T) {
	s := &syncSource{policy: alicePolicy}
	a := newSyncAuthorizer(t, s)
	s.set("", errors.New("temporary outage"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reported := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- a.RunPolicyReload(ctx, 5*time.Millisecond, func(error) {
			select {
			case reported <- struct{}{}:
			default:
			}
		})
	}()
	select {
	case <-reported:
	case <-time.After(time.Second):
		t.Fatal("error not reported")
	}
	assertAlice(t, a, true)
	s.set("", nil)
	deadline := time.After(time.Second)
	for {
		ok, err := a.Enforce("alice", "/reports", "GET")
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		select {
		case <-deadline:
			t.Fatal("next tick failed to repair")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
