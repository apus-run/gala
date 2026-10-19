package authz

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/casbin/casbin/v3/model"
	"github.com/casbin/casbin/v3/persist"
)

// The fake clock exercises the exact boundary through public APIs without
// changing the authorizer's stored timestamp or adding production clock hooks.
func TestPolicyExpiresAtExactMaximumAge(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const age = time.Second
		a := newSyncAuthorizer(t, &syncSource{policy: alicePolicy}, WithMaxPolicyAge(age))
		time.Sleep(age - time.Nanosecond)
		assertAlice(t, a, true)
		if _, err := a.GetRolesForUser("alice"); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Nanosecond)
		if allowed, err := a.Enforce("alice", "/reports", "GET"); allowed || !errors.Is(err, ErrPolicyStale) {
			t.Fatalf("at T: allowed=%v err=%v", allowed, err)
		}
		if roles, err := a.GetRolesForUser("alice"); roles != nil || !errors.Is(err, ErrPolicyStale) {
			t.Fatalf("at T: roles=%v err=%v", roles, err)
		}
	})
}

func TestSuccessfulSlowLoadDoesNotRenewAgeFromCompletion(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const age = time.Second
		var delay time.Duration
		adapter := &reloadAdapter{load: func(m model.Model) error { time.Sleep(delay); return persist.LoadPolicyLine(alicePolicy, m) }}
		a := newSyncAuthorizer(t, adapter, WithMaxPolicyAge(age))
		delay = age
		if err := a.LoadPolicy(); err != nil {
			t.Fatal(err)
		}
		if allowed, err := a.Enforce("alice", "/reports", "GET"); allowed || !errors.Is(err, ErrPolicyStale) {
			t.Fatalf("slow successful load: allowed=%v err=%v", allowed, err)
		}
		delay = 0
		if err := a.LoadPolicy(); err != nil {
			t.Fatal(err)
		}
		assertAlice(t, a, true)
	})
}

func TestReloadTickerUsesFixedTicksNotPostLoadDelay(t *testing.T) {
	for _, duration := range []time.Duration{3 * time.Second, 15 * time.Second} {
		t.Run(duration.String(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				const interval = 10 * time.Second
				starts := make(chan time.Time, 3)
				recording := false
				adapter := &reloadAdapter{load: func(m model.Model) error {
					if recording {
						starts <- time.Now()
						time.Sleep(duration)
					}
					return persist.LoadPolicyLine(alicePolicy, m)
				}}
				a := newSyncAuthorizer(t, adapter)
				recording = true
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				done := make(chan error, 1)
				go func() { done <- a.RunPolicyReload(ctx, interval, func(err error) { t.Error(err) }) }()
				first := <-starts
				second := <-starts
				third := <-starts
				cancel()
				if err := <-done; !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				want := max(interval, duration)
				if second.Sub(first) != want || third.Sub(second) != want {
					t.Fatalf("start intervals=%v,%v want=%v", second.Sub(first), third.Sub(second), want)
				}
			})
		})
	}
}

func TestReloadIntervalAtMaximumAgeRejectedBeforeLoading(t *testing.T) {
	calls := 0
	adapter := &reloadAdapter{load: func(m model.Model) error { calls++; return persist.LoadPolicyLine(alicePolicy, m) }}
	a := newSyncAuthorizer(t, adapter, WithMaxPolicyAge(time.Second))
	for _, interval := range []time.Duration{time.Second, 2 * time.Second} {
		if err := a.RunPolicyReload(context.Background(), interval, func(error) {}); err == nil {
			t.Fatal("invalid interval accepted")
		}
	}
	if calls != 1 {
		t.Fatalf("invalid loop accessed storage: loads=%d", calls)
	}
}

func TestWatcherCatchUpFailureDoesNotReturnAuthorizer(t *testing.T) {
	failure := errors.New("catch-up read failed")
	source := &syncSource{policy: alicePolicy}
	watcher := &testWatcher{register: func() { source.set("", failure) }}
	a, err := New(WithModelFilePath(writeModelFile(t, testAllowModel)), WithPolicyAdapter(source), WithWatcher(watcher), WithWatcherErrorHandler(func(err error) { t.Error(err) }))
	if a != nil || !errors.Is(err, failure) || !strings.Contains(err.Error(), "authz: watcher catch-up") {
		t.Fatalf("a=%v err=%v", a, err)
	}
}
