package ws

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestWithDialerNilKeepsDefaultDialer(t *testing.T) {
	serverURL := newWSTestServer(t, func(context.Context, *Conn) {})

	client, err := NewClient(serverURL, WithDialer(nil))
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	t.Cleanup(func() {
		_ = client.CloseConnection()
	})

	if client.GetConnection() == nil {
		t.Fatal("connection is nil")
	}
}

func TestClientConnectConcurrentAccess(t *testing.T) {
	serverURL := newWSTestServer(t, func(ctx context.Context, conn *Conn) {
		<-ctx.Done()
	})

	client, err := NewClient(serverURL)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	t.Cleanup(func() {
		_ = client.CloseConnection()
	})

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for range 20 {
			_ = client.GetConnection()
		}
	}()

	go func() {
		defer wg.Done()
		for range 20 {
			if err := client.connect(); err != nil {
				t.Errorf("connect() error = %v", err)
				return
			}
		}
	}()

	wg.Wait()
}

func TestWithUpgraderNilKeepsDefaultUpgrader(t *testing.T) {
	serverURL := newWSTestServer(t, nil, WithUpgrader(nil))

	client, err := NewClient(serverURL)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	_ = client.CloseConnection()
}

func newWSTestServer(t *testing.T, loop LoopFunc, opts ...ServerOption) string {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		srv := NewServer(w, r, loop, opts...)
		if err := srv.Run(r.Context()); err != nil {
			t.Errorf("Run() error = %v", err)
		}
	}))
	t.Cleanup(server.Close)

	return "ws" + strings.TrimPrefix(server.URL, "http")
}
