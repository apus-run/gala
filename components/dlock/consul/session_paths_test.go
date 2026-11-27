package consul

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/apus-run/gala/components/dlock"
	"github.com/hashicorp/consul/api"
)

type sessionTransport struct {
	create, acquire, renew, read, release string
}

func (s sessionTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	var body string
	switch {
	case strings.Contains(r.URL.Path, "/session/create"):
		body = s.create
	case strings.Contains(r.URL.Path, "/session/destroy"):
		body = "true"
	case strings.Contains(r.URL.Path, "/session/renew"):
		body = s.renew
	case r.URL.Query().Has("acquire"):
		body = s.acquire
	case r.URL.Query().Has("release"):
		body = s.release
	case r.Method == http.MethodGet:
		body = s.read
	default:
		return nil, errors.New("unexpected Consul request")
	}
	code := http.StatusOK
	switch body {
	case "error":
		code = http.StatusServiceUnavailable
	case "gone":
		code = http.StatusNotFound
	}
	return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
}

func TestSessionTransitions(t *testing.T) {
	const live = `[{"ID":"s"}]`
	const owned = `[{"Key":"k","Session":"s","Value":"cGVlckB0b2tlbg=="}]`
	const created = `{"ID":"new"}`
	for _, tc := range []struct {
		name      string
		operation func(*Lock, context.Context) error
		held      bool
		reply     sessionTransport
		sentinel  error
		message   string
		wantHeld  bool
	}{
		{name: "create unavailable", operation: (*Lock).Lock, reply: sessionTransport{create: "error"}, message: "create session"},
		{name: "acquire unavailable", operation: (*Lock).Lock, reply: sessionTransport{create: created, acquire: "error"}, message: "acquire lock"},
		{name: "known holder", operation: (*Lock).Lock, reply: sessionTransport{create: created, acquire: "false", read: owned}, sentinel: dlock.ErrLocked, message: "peer"},
		{name: "unknown holder", operation: (*Lock).Lock, reply: sessionTransport{create: created, acquire: "false", read: "gone"}, sentinel: dlock.ErrLocked},
		{name: "idempotent lock", operation: (*Lock).Lock, held: true, reply: sessionTransport{renew: live, read: owned}, wantHeld: true},
		{name: "idempotent read fails", operation: (*Lock).Lock, held: true, reply: sessionTransport{renew: live, read: "error"}, message: "verify lock", wantHeld: true},
		{name: "idempotent renew fails", operation: (*Lock).Lock, held: true, reply: sessionTransport{renew: "error", read: owned}, message: "refresh lock", wantHeld: true},
		{name: "reacquire after loss", operation: (*Lock).Lock, held: true, reply: sessionTransport{renew: "gone", read: "gone", create: created, acquire: "true"}, wantHeld: true},
		{name: "unlock without session", operation: (*Lock).Unlock, sentinel: dlock.ErrLockNotHeld},
		{name: "release unavailable", operation: (*Lock).Unlock, held: true, reply: sessionTransport{release: "error", read: owned}, message: "release lock", wantHeld: true},
		{name: "release after loss", operation: (*Lock).Unlock, held: true, reply: sessionTransport{release: "false"}, sentinel: dlock.ErrLockNotHeld},
		{name: "refresh without session", operation: (*Lock).Refresh, sentinel: dlock.ErrLockNotHeld},
		{name: "renew fails after loss", operation: (*Lock).Refresh, held: true, reply: sessionTransport{renew: "error", read: "gone"}, sentinel: dlock.ErrLockNotHeld},
		{name: "renew temporarily fails", operation: (*Lock).Refresh, held: true, reply: sessionTransport{renew: "error", read: owned}, message: "refresh lock", wantHeld: true},
		{name: "renew cannot verify", operation: (*Lock).Refresh, held: true, reply: sessionTransport{renew: live, read: "error"}, message: "verify lock", wantHeld: true},
		{name: "renew and verify", operation: (*Lock).Refresh, held: true, reply: sessionTransport{renew: live, read: owned}, wantHeld: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := api.NewClient(&api.Config{HttpClient: &http.Client{Transport: tc.reply}})
			if err != nil {
				t.Fatal(err)
			}
			locker, err := NewClient(c).NewLock(t.Context(), dlock.WithLockName("k"))
			if err != nil {
				t.Fatal(err)
			}
			l := locker.(*Lock)
			l.held = tc.held
			if tc.held {
				l.sessionID = "s"
			}
			err = tc.operation(l, t.Context())
			if tc.sentinel != nil && !errors.Is(err, tc.sentinel) {
				t.Fatalf("want %v, got %v", tc.sentinel, err)
			}
			if tc.message != "" && (err == nil || !strings.Contains(err.Error(), tc.message)) {
				t.Fatalf("want %q, got %v", tc.message, err)
			}
			if tc.sentinel == nil && tc.message == "" && err != nil {
				t.Fatal(err)
			}
			if l.held != tc.wantHeld {
				t.Fatalf("held=%v", l.held)
			}
		})
	}
}

func TestSessionQueueDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client, err := api.NewClient(api.DefaultConfig())
		if err != nil {
			t.Fatal(err)
		}
		f := NewClient(client)
		if l, err := f.NewLock(t.Context(), dlock.WithLockName("")); l != nil || err == nil {
			t.Fatalf("invalid lock %v %v", l, err)
		}
		locker, err := f.NewLock(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		l := locker.(*Lock)
		if err := l.mu.Acquire(t.Context(), 1); err != nil {
			t.Fatal(err)
		}
		defer l.mu.Release(1)
		for _, op := range []func(context.Context) error{l.Lock, l.Refresh, l.Unlock} {
			if err := op(t.Context()); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("queued operation: %v", err)
			}
		}
	})
}
