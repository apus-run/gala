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
	mu          sync.Mutex
	renewStatus int
	keySession  string
	released    bool
}

func (f *fakeConsul) wasReleased() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.released
}

func newTestClient(t *testing.T, fake *fakeConsul) *api.Client {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/session/create", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"ID":"session-1"}`)
	})
	mux.HandleFunc("/v1/session/renew/session-1", func(w http.ResponseWriter, _ *http.Request) {
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
	mux.HandleFunc("/v1/session/destroy/session-1", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, "true")
	})
	mux.HandleFunc("/v1/kv/lock", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			if r.URL.Query().Get("release") != "" {
				fake.mu.Lock()
				fake.released = true
				fake.mu.Unlock()
			}
			_, _ = fmt.Fprint(w, "true")
		case http.MethodGet:
			fake.mu.Lock()
			session := fake.keySession
			fake.mu.Unlock()
			if session == "" {
				session = "session-1"
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `[{"Key":"lock","Session":%q}]`, session)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	client, err := api.NewClient(&api.Config{Address: server.Listener.Addr().String()})
	if err != nil {
		t.Fatalf("api.NewClient() error = %v", err)
	}
	return client
}
