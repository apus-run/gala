package redis_test

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"

	"github.com/apus-run/gala/components/dlock"
	"github.com/apus-run/gala/components/dlock/redis"
)

func newLocker(t *testing.T, srv *miniredis.Miniredis, opts ...dlock.Option) dlock.Locker {
	t.Helper()

	client := goredis.NewClient(&goredis.Options{Addr: srv.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	locker, err := redis.NewLock(context.Background(), client, opts...)
	if err != nil {
		t.Fatalf("NewLock() error = %v", err)
	}
	return locker
}

func TestNewLockValidation(t *testing.T) {
	srv := miniredis.RunT(t)

	tests := []struct {
		name string
		opts []dlock.Option
	}{
		{name: "empty name", opts: []dlock.Option{dlock.WithLockName("")}},
		{name: "zero timeout", opts: []dlock.Option{dlock.WithLockTimeout(0)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := redis.NewLock(context.Background(), goredis.NewClient(&goredis.Options{Addr: srv.Addr()}), tt.opts...); err == nil {
				t.Fatal("NewLock() error = nil, want validation error")
			}
		})
	}

	if _, err := redis.NewLock(context.Background(), nil); err == nil {
		t.Fatal("NewLock(nil) error = nil, want error")
	}
}

func TestLockIsExclusive(t *testing.T) {
	srv := miniredis.RunT(t)
	ctx := context.Background()

	first := newLocker(t, srv, dlock.WithLockName("exclusive"), dlock.WithOwnerID("owner-a"))
	second := newLocker(t, srv, dlock.WithLockName("exclusive"), dlock.WithOwnerID("owner-b"))

	if err := first.Lock(ctx); err != nil {
		t.Fatalf("first Lock() error = %v", err)
	}

	err := second.Lock(ctx)
	if !errors.Is(err, dlock.ErrLocked) {
		t.Fatalf("second Lock() error = %v, want ErrLocked", err)
	}
	if !strings.Contains(err.Error(), "owner-a") {
		t.Fatalf("second Lock() error = %v, want it to name the holder", err)
	}

	if err := first.Unlock(ctx); err != nil {
		t.Fatalf("first Unlock() error = %v", err)
	}
	if err := second.Lock(ctx); err != nil {
		t.Fatalf("second Lock() after release error = %v", err)
	}
}

func TestOperationTimeoutBoundsLock(t *testing.T) {
	addr := stallingRedis(t)
	client := goredis.NewClient(&goredis.Options{
		Addr: addr,
		// go-redis only applies the context deadline to socket I/O when this
		// is enabled; otherwise ReadTimeout/WriteTimeout win.
		// go-redis 默认不把 ctx 截止时间用于 socket 读写；开启后 OperationTimeout
		// 才能真正取消卡住的读取。
		ContextTimeoutEnabled: true,
	})
	t.Cleanup(func() { _ = client.Close() })

	locker, err := redis.NewLock(context.Background(), client,
		dlock.WithLockName("timeout"),
		dlock.WithOperationTimeout(50*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("NewLock() error = %v", err)
	}

	start := time.Now()
	err = locker.Lock(context.Background())
	if err == nil {
		t.Fatal("Lock() error = nil, want a timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Lock() error = %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Lock() took %s, want it bounded by the 50ms operation timeout", elapsed)
	}
}

// stallingRedis starts a TCP listener that accepts connections and never
// replies, so a Redis command blocks until its context expires.
// stallingRedis 启动一个只接受连接、从不回复的 TCP 监听器，让 Redis 命令一直
// 阻塞到上下文超时。
func stallingRedis(t *testing.T) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	var (
		mu    sync.Mutex
		conns []net.Conn
	)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, conn)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		for _, conn := range conns {
			_ = conn.Close()
		}
	})

	return ln.Addr().String()
}

func TestMillisecondTTL(t *testing.T) {
	srv := miniredis.RunT(t)

	locker := newLocker(t, srv, dlock.WithLockName("ttl"), dlock.WithLockTimeout(1500*time.Millisecond))
	if err := locker.Lock(context.Background()); err != nil {
		t.Fatalf("Lock() error = %v", err)
	}
	if ttl := srv.TTL("ttl"); ttl != 1500*time.Millisecond {
		t.Fatalf("TTL = %s, want 1.5s", ttl)
	}
}

func TestClientNewLockReturnsNilLockerOnError(t *testing.T) {
	srv := miniredis.RunT(t)
	cli := goredis.NewClient(&goredis.Options{Addr: srv.Addr()})
	t.Cleanup(func() { _ = cli.Close() })

	locker, err := redis.NewClient(cli).NewLock(context.Background(), dlock.WithLockName(""))
	if err == nil {
		t.Fatal("NewLock() error = nil, want validation error")
	}
	if locker != nil {
		t.Fatal("NewLock() returned a non-nil Locker on error")
	}
}

func TestUnlockIgnoresCanceledContext(t *testing.T) {
	srv := miniredis.RunT(t)

	locker := newLocker(t, srv, dlock.WithLockName("canceled"), dlock.WithLockTimeout(time.Minute))
	if err := locker.Lock(context.Background()); err != nil {
		t.Fatalf("Lock() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := locker.Unlock(ctx); err != nil {
		t.Fatalf("Unlock() with canceled ctx error = %v, want nil", err)
	}
	if srv.Exists("canceled") {
		t.Fatal("Unlock() with canceled ctx left the key behind")
	}
}

func TestNewLockRejectsTypedNilClient(t *testing.T) {
	var rdb *goredis.Client
	if _, err := redis.NewLock(context.Background(), rdb); err == nil {
		t.Fatal("NewLock(typed nil) error = nil, want error")
	}
}

func TestLockIsIdempotentForSameLocker(t *testing.T) {
	srv := miniredis.RunT(t)
	ctx := context.Background()

	locker := newLocker(t, srv, dlock.WithLockName("idempotent"), dlock.WithLockTimeout(time.Minute))
	if err := locker.Lock(ctx); err != nil {
		t.Fatalf("Lock() error = %v", err)
	}

	srv.FastForward(40 * time.Second)
	if err := locker.Lock(ctx); err != nil {
		t.Fatalf("second Lock() error = %v, want idempotent success", err)
	}
	if ttl := srv.TTL("idempotent"); ttl != time.Minute {
		t.Fatalf("TTL after second Lock() = %s, want 1m", ttl)
	}
}

func TestUnlockByStaleHolder(t *testing.T) {
	srv := miniredis.RunT(t)
	ctx := context.Background()

	stale := newLocker(t, srv, dlock.WithLockName("stale"), dlock.WithLockTimeout(5*time.Second))
	if err := stale.Lock(ctx); err != nil {
		t.Fatalf("stale Lock() error = %v", err)
	}

	srv.FastForward(6 * time.Second)

	current := newLocker(t, srv, dlock.WithLockName("stale"), dlock.WithLockTimeout(time.Minute))
	if err := current.Lock(ctx); err != nil {
		t.Fatalf("current Lock() error = %v", err)
	}

	if err := stale.Unlock(ctx); !errors.Is(err, dlock.ErrLockNotHeld) {
		t.Fatalf("stale Unlock() error = %v, want ErrLockNotHeld", err)
	}
	if !srv.Exists("stale") {
		t.Fatal("stale Unlock() removed the current holder's key")
	}

	if err := current.Refresh(ctx); err != nil {
		t.Fatalf("current Refresh() error = %v", err)
	}
}

func TestRefreshByStaleHolder(t *testing.T) {
	srv := miniredis.RunT(t)
	ctx := context.Background()

	stale := newLocker(t, srv, dlock.WithLockName("refresh"), dlock.WithLockTimeout(5*time.Second))
	if err := stale.Lock(ctx); err != nil {
		t.Fatalf("stale Lock() error = %v", err)
	}

	srv.FastForward(6 * time.Second)

	current := newLocker(t, srv, dlock.WithLockName("refresh"), dlock.WithLockTimeout(time.Minute))
	if err := current.Lock(ctx); err != nil {
		t.Fatalf("current Lock() error = %v", err)
	}

	if err := stale.Refresh(ctx); !errors.Is(err, dlock.ErrLockNotHeld) {
		t.Fatalf("stale Refresh() error = %v, want ErrLockNotHeld", err)
	}
	if ttl := srv.TTL("refresh"); ttl > time.Minute {
		t.Fatalf("TTL = %s, stale Refresh() extended the current holder's lock", ttl)
	}
}

func TestRefreshExtendsTTL(t *testing.T) {
	srv := miniredis.RunT(t)
	ctx := context.Background()

	locker := newLocker(t, srv, dlock.WithLockName("extend"), dlock.WithLockTimeout(time.Minute))
	if err := locker.Lock(ctx); err != nil {
		t.Fatalf("Lock() error = %v", err)
	}

	srv.FastForward(30 * time.Second)
	if err := locker.Refresh(ctx); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if ttl := srv.TTL("extend"); ttl != time.Minute {
		t.Fatalf("TTL after Refresh() = %s, want 1m", ttl)
	}
}

func TestLockAfterExpiry(t *testing.T) {
	srv := miniredis.RunT(t)
	ctx := context.Background()

	first := newLocker(t, srv, dlock.WithLockName("expiry"), dlock.WithLockTimeout(5*time.Second))
	if err := first.Lock(ctx); err != nil {
		t.Fatalf("first Lock() error = %v", err)
	}

	srv.FastForward(6 * time.Second)

	second := newLocker(t, srv, dlock.WithLockName("expiry"), dlock.WithLockTimeout(time.Minute))
	if err := second.Lock(ctx); err != nil {
		t.Fatalf("second Lock() after expiry error = %v", err)
	}
}

func TestUnlockWithoutHolding(t *testing.T) {
	srv := miniredis.RunT(t)
	ctx := context.Background()

	locker := newLocker(t, srv, dlock.WithLockName("missing"))
	_ = locker.Lock(ctx)

	srv.FastForward(time.Hour)

	if err := locker.Unlock(ctx); !errors.Is(err, dlock.ErrLockNotHeld) {
		t.Fatalf("Unlock() on missing key error = %v, want ErrLockNotHeld", err)
	}
}

func TestRedisFailurePaths(t *testing.T) {
	srv := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: srv.Addr(), MaxRetries: -1})
	defer client.Close()
	locker, err := redis.NewLock(t.Context(), client, dlock.WithLockName("held"))
	if err != nil {
		t.Fatal(err)
	}
	// The Lua script can see the competing key even when the client is denied
	// the separate diagnostic GET. Contention must keep its sentinel.
	srv.Set("held", "peer@token")
	client.AddHook(denyGet{})
	if err := locker.Lock(t.Context()); !errors.Is(err, dlock.ErrLocked) || strings.Contains(err.Error(), "peer") {
		t.Fatalf("contention without diagnostic: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	for _, op := range []func(context.Context) error{locker.Refresh, locker.Unlock} {
		if err := op(t.Context()); !errors.Is(err, goredis.ErrClosed) {
			t.Fatalf("closed store: %v", err)
		}
	}
}

type denyGet struct{}

func (denyGet) DialHook(next goredis.DialHook) goredis.DialHook { return next }

func (denyGet) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return next
}

func (denyGet) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		if cmd.Name() == "get" {
			return errors.New("diagnostic read denied")
		}
		return next(ctx, cmd)
	}
}
