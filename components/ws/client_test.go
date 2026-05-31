package ws

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
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

	wg.Go(func() {
		for range 20 {
			_ = client.GetConnection()
		}
	})

	wg.Go(func() {
		for range 20 {
			if err := client.connect(); err != nil {
				t.Errorf("connect() error = %v", err)
				return
			}
		}
	})

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

func TestDefaultUpgrader(t *testing.T) {
	upgrader := DefaultUpgrader()

	if upgrader.HandshakeTimeout != 10*time.Second {
		t.Fatalf("HandshakeTimeout = %v, want 10s", upgrader.HandshakeTimeout)
	}
	if upgrader.ReadBufferSize != 0 {
		t.Fatalf("ReadBufferSize = %d, want 0", upgrader.ReadBufferSize)
	}
	if upgrader.WriteBufferSize != 0 {
		t.Fatalf("WriteBufferSize = %d, want 0", upgrader.WriteBufferSize)
	}
	if upgrader.CheckOrigin != nil {
		t.Fatal("default upgrader overrides gorilla/websocket origin validation")
	}
}

func TestServerNoClientPingTimeoutRepliesPong(t *testing.T) {
	serverURL := newWSTestServer(t, func(_ context.Context, conn *Conn) {
		_, _, _ = conn.ReadMessage()
	}, WithNoClientPingTimeout(time.Second))

	client, err := NewClient(serverURL)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	t.Cleanup(func() {
		_ = client.CloseConnection()
	})

	pong := make(chan struct{}, 1)
	conn := client.GetConnection()
	conn.SetPongHandler(func(string) error {
		pong <- struct{}{}
		return nil
	})

	go func() {
		_, _, _ = conn.ReadMessage()
	}()

	if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(time.Second)); err != nil {
		t.Fatalf("WriteControl() error = %v", err)
	}

	select {
	case <-pong:
	case <-time.After(time.Second):
		t.Fatal("server did not reply to ping with pong")
	}
}

func TestCloseConnectionRejectsCompletedReconnect(t *testing.T) {
	serverURL := newWSTestServer(t, func(_ context.Context, conn *Conn) {
		_, _, _ = conn.ReadMessage()
	})
	client, err := NewClient(serverURL)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	oldConn := client.GetConnection()

	requestStarted := make(chan struct{})
	releaseHandshake := make(chan struct{})
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() {
			close(releaseHandshake)
		})
	}
	t.Cleanup(release)

	blockedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requestStarted)
		<-releaseHandshake
		conn, upgradeErr := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if upgradeErr == nil {
			_ = conn.Close()
		}
	}))
	t.Cleanup(blockedServer.Close)

	client.url = "ws" + strings.TrimPrefix(blockedServer.URL, "http")
	reconnectDone := make(chan error, 1)
	go func() {
		reconnectDone <- client.TryReconnect()
	}()

	select {
	case <-requestStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("reconnect request did not start")
	}

	if err := client.CloseConnection(); err != nil {
		t.Fatalf("CloseConnection() error = %v", err)
	}
	release()

	select {
	case err := <-reconnectDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("TryReconnect() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("TryReconnect() did not reject the completed connection after CloseConnection()")
	}

	if conn := client.getConnection(); conn != oldConn {
		t.Fatal("reconnect replaced the connection after CloseConnection()")
	}
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
