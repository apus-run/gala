package consul_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/consul/api"

	"github.com/apus-run/gala/components/dlock"
	"github.com/apus-run/gala/components/dlock/consul"
)

func TestNewLockValidation(t *testing.T) {
	if _, err := consul.NewLock(context.Background(), nil); err == nil {
		t.Fatal("NewLock(nil) error = nil, want error")
	}

	client := newTestClient(t, &fakeConsul{})

	if _, err := consul.NewLock(context.Background(), client, dlock.WithLockTimeout(9*time.Second)); err == nil {
		t.Fatal("NewLock(9s) error = nil, want minimum-TTL error")
	} else if !strings.Contains(err.Error(), "10s") {
		t.Fatalf("NewLock(9s) error = %v, want it to mention 10s", err)
	}

	if _, err := consul.NewLock(context.Background(), client, dlock.WithLockTimeout(10*time.Second)); err != nil {
		t.Fatalf("NewLock(10s) error = %v", err)
	}

	if _, err := consul.NewLock(context.Background(), client, dlock.WithLockTimeout(25*time.Hour)); err == nil {
		t.Fatal("NewLock(25h) error = nil, want maximum-TTL error")
	}
}

func TestRefreshAfterSessionGone(t *testing.T) {
	fake := &fakeConsul{renewStatus: http.StatusNotFound}
	client := newTestClient(t, fake)
	ctx := context.Background()

	locker, err := consul.NewLock(ctx, client, dlock.WithLockName("lock"), dlock.WithLockTimeout(10*time.Second))
	if err != nil {
		t.Fatalf("NewLock() error = %v", err)
	}
	if err := locker.Lock(ctx); err != nil {
		t.Fatalf("Lock() error = %v", err)
	}

	// Consul answers Renew with 404 and a nil entry; this must be reported as
	// a lost lock, not a successful renewal.
	if err := locker.Refresh(ctx); !errors.Is(err, dlock.ErrLockNotHeld) {
		t.Fatalf("Refresh() error = %v, want ErrLockNotHeld", err)
	}
}

func TestRefreshAfterKeyLost(t *testing.T) {
	fake := &fakeConsul{renewStatus: http.StatusOK, keySession: "another-session"}
	client := newTestClient(t, fake)
	ctx := context.Background()

	locker, err := consul.NewLock(ctx, client, dlock.WithLockName("lock"), dlock.WithLockTimeout(10*time.Second))
	if err != nil {
		t.Fatalf("NewLock() error = %v", err)
	}
	if err := locker.Lock(ctx); err != nil {
		t.Fatalf("Lock() error = %v", err)
	}

	// The session is still renewable, but the key now belongs to someone else.
	if err := locker.Refresh(ctx); !errors.Is(err, dlock.ErrLockNotHeld) {
		t.Fatalf("Refresh() error = %v, want ErrLockNotHeld", err)
	}
}

func TestUnlockWithCanceledContext(t *testing.T) {
	fake := &fakeConsul{renewStatus: http.StatusOK, keySession: "session-1"}
	client := newTestClient(t, fake)
	ctx := context.Background()

	locker, err := consul.NewLock(ctx, client, dlock.WithLockName("lock"), dlock.WithLockTimeout(10*time.Second))
	if err != nil {
		t.Fatalf("NewLock() error = %v", err)
	}
	if err := locker.Lock(ctx); err != nil {
		t.Fatalf("Lock() error = %v", err)
	}

	canceled, cancel := context.WithCancel(ctx)
	cancel()

	if err := locker.Unlock(canceled); err != nil {
		t.Fatalf("Unlock() with canceled ctx error = %v, want nil", err)
	}
	if !fake.wasReleased() {
		t.Fatal("Unlock() with canceled ctx did not send KV.Release")
	}
}

type fakeConsul struct {
	mu           sync.Mutex
	renewStatus  int
	keySession   string
	releaseFails bool
	released     bool
	destroyed    int
}

func (f *fakeConsul) wasReleased() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.released
}

func (f *fakeConsul) wasDestroyed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.destroyed > 0
}

func newTestClient(t *testing.T, fake *fakeConsul) *api.Client {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("PUT /v1/session/create", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"ID":"session-1"}`)
	})
	mux.HandleFunc("PUT /v1/session/renew/session-1", func(w http.ResponseWriter, _ *http.Request) {
		fake.mu.Lock()
		status := fake.renewStatus
		fake.mu.Unlock()
		if status == 0 {
			status = http.StatusOK
		}
		w.Header().Set("Content-Type", "application/json")
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		_, _ = fmt.Fprint(w, `[{"ID":"session-1","TTL":"10s"}]`)
	})
	mux.HandleFunc("PUT /v1/session/destroy/session-1", func(w http.ResponseWriter, _ *http.Request) {
		fake.mu.Lock()
		fake.destroyed++
		fake.mu.Unlock()
		_, _ = fmt.Fprint(w, "true")
	})
	mux.HandleFunc("PUT /v1/kv/lock", func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		if r.URL.Query().Get("release") != "" {
			if fake.releaseFails {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			fake.released = true
		}
		_, _ = fmt.Fprint(w, "true")
	})
	mux.HandleFunc("GET /v1/kv/lock", func(w http.ResponseWriter, _ *http.Request) {
		fake.mu.Lock()
		session := fake.keySession
		fake.mu.Unlock()
		if session == "" {
			session = "session-1"
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `[{"Key":"lock","Session":%q}]`, session)
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	client, err := api.NewClient(&api.Config{Address: server.Listener.Addr().String()})
	if err != nil {
		t.Fatalf("api.NewClient() error = %v", err)
	}
	return client
}

func TestUnlockAfterKeyReassignedDestroysSession(t *testing.T) {
	fake := &fakeConsul{renewStatus: http.StatusOK, keySession: "another-session", releaseFails: true}
	client := newTestClient(t, fake)
	ctx := context.Background()

	locker, err := consul.NewLock(ctx, client, dlock.WithLockName("lock"), dlock.WithLockTimeout(10*time.Second))
	if err != nil {
		t.Fatalf("NewLock() error = %v", err)
	}
	if err := locker.Lock(ctx); err != nil {
		t.Fatalf("Lock() error = %v", err)
	}

	// KV.Release fails and the ownership read shows the key belongs to another
	// session: Unlock must report ErrLockNotHeld and still destroy our session.
	// KV.Release 失败且所有权读显示 key 属于其他 session：Unlock 必须返回
	// ErrLockNotHeld，并且仍然销毁本 session。
	if err := locker.Unlock(ctx); !errors.Is(err, dlock.ErrLockNotHeld) {
		t.Fatalf("Unlock() error = %v, want ErrLockNotHeld", err)
	}
	if !fake.wasDestroyed() {
		t.Fatal("Unlock() left the live session behind after losing the key")
	}
}
