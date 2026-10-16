package redisstore

import (
	"bytes"
	"context"
	"io"
	"math"
	"net"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/apus-run/gala/components/ecache"
	"github.com/apus-run/gala/components/layercache"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestStoreRoundTrip(t *testing.T) {
	for _, protocol := range []int{2, 3} {
		t.Run(string(rune('0'+protocol)), func(t *testing.T) {
			server := miniredis.RunT(t)
			client := redis.NewClient(&redis.Options{Addr: server.Addr(), Protocol: protocol, ContextTimeoutEnabled: true, MaxRetries: -1})
			t.Cleanup(func() { require.NoError(t, client.Close()) })
			store, err := New(client)
			require.NoError(t, err)
			ctx := context.Background()
			_, err = store.Get(ctx, "absent")
			require.ErrorIs(t, err, layercache.ErrMiss)
			writeDeadline, err := store.Set(ctx, "key", []byte("value"), time.Second+999*time.Microsecond)
			require.NoError(t, err)
			require.True(t, writeDeadline.Before(time.Now().Add(time.Second)))
			require.Equal(t, time.Second, server.TTL("key"), "Redis TTL must be rounded down")
			entry, err := store.Get(ctx, "key")
			require.NoError(t, err)
			require.Equal(t, []byte("value"), entry.Data)
			require.True(t, entry.ExpiresAt.Before(time.Now().Add(time.Second)))
			require.Positive(t, time.Until(entry.ExpiresAt))
			server.FastForward(time.Second)
			_, err = store.Get(ctx, "key")
			require.ErrorIs(t, err, layercache.ErrMiss)
			require.NoError(t, server.Set("persistent", "external"))
			_, err = store.Get(ctx, "persistent")
			require.ErrorIs(t, err, layercache.ErrInvalidTTL)
			for _, ttl := range []time.Duration{0, -time.Second, time.Millisecond - 1} {
				_, err := store.Set(ctx, "invalid", nil, ttl)
				require.ErrorIs(t, err, layercache.ErrInvalidTTL)
				require.False(t, server.Exists("invalid"))
			}
			require.NoError(t, store.Delete(ctx, "persistent"))
			require.False(t, server.Exists("persistent"))
			require.NoError(t, store.Delete(ctx, "absent"))
			// GET against a different Redis type is a backend error, never a miss.
			_, err = server.Lpush("list", "x")
			require.NoError(t, err)
			_, err = store.Get(ctx, "list")
			require.Error(t, err)
			require.NotErrorIs(t, err, layercache.ErrMiss)
			cancelled, cancel := context.WithCancel(ctx)
			cancel()
			_, err = store.Set(cancelled, "cancelled", nil, time.Second)
			require.ErrorIs(t, err, context.Canceled)
		})
	}
}

func TestLayercacheIntegration(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), ContextTimeoutEnabled: true, MaxRetries: -1})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	store, err := New(client)
	require.NoError(t, err)
	local := ecache.New[string, layercache.Entry](ecache.WithMaxKeys(10))
	base, err := layercache.New(layercache.WithLocal(local), layercache.WithRemote(store), layercache.WithNamespace("users:v1"), layercache.WithNotFoundTTL(time.Second))
	c := layercache.NewTyped[string](base)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, c.Close()) })
	ctx := context.Background()
	calls := 0
	load := func(context.Context, string) (string, error) { calls++; return "alice", nil }
	got, err := c.GetOrLoad(ctx, "user:1", load)
	require.NoError(t, err)
	require.Equal(t, "alice", got)
	require.NoError(t, c.InvalidateLocal(ctx, "user:1"))
	got, err = c.GetOrLoad(ctx, "user:1", load)
	require.NoError(t, err)
	require.Equal(t, "alice", got)
	require.Equal(t, 1, calls)
	_, err = c.GetOrLoad(ctx, "user:404", func(context.Context, string) (string, error) { return "", layercache.ErrNotFound })
	require.ErrorIs(t, err, layercache.ErrNotFound)
	require.NoError(t, c.InvalidateLocal(ctx, "user:404"))
	_, err = c.Get(ctx, "user:404")
	require.ErrorIs(t, err, layercache.ErrNotFound)
	require.NoError(t, c.Delete(ctx, "user:1"))
	_, err = c.Get(ctx, "user:1")
	require.ErrorIs(t, err, layercache.ErrMiss)
	require.NoError(t, c.Close())
	// Coordinator closure must neither purge Redis nor close the application client.
	require.NoError(t, client.Ping(ctx).Err())
	require.Len(t, server.Keys(), 1)
}

func TestParseSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name     string
		response any
		want     error
	}{
		{"missing", []any{nil, int64(-2)}, layercache.ErrMiss},
		{"persistent", []any{"v", int64(-1)}, layercache.ErrInvalidTTL},
		{"zero", []any{"v", int64(0)}, layercache.ErrMiss},
		{"precision boundary", []any{"v", int64(1)}, layercache.ErrMiss},
		{"invalid negative", []any{"v", int64(-3)}, layercache.ErrInvalidTTL},
		{"overflow", []any{"v", int64(math.MaxInt64)}, layercache.ErrInvalidTTL},
		{"wrong shape", nil, layercache.ErrCorrupt},
		{"wrong length", []any{"v"}, layercache.ErrCorrupt},
		{"wrong PTTL", []any{"v", "100"}, layercache.ErrCorrupt},
		{"wrong value", []any{false, int64(100)}, layercache.ErrCorrupt},
	} {
		t.Run(tc.name, func(t *testing.T) { _, err := parseSnapshot(tc.response, time.Now()); require.ErrorIs(t, err, tc.want) })
	}
	synctest.Test(t, func(t *testing.T) {
		started := time.Now()
		time.Sleep(20 * time.Millisecond)
		entry, err := parseSnapshot([]any{[]byte("v"), int64(100)}, started)
		require.NoError(t, err)
		require.Equal(t, 79*time.Millisecond, time.Until(entry.ExpiresAt))
		time.Sleep(79 * time.Millisecond)
		_, err = parseSnapshot([]any{"v", int64(100)}, started)
		require.ErrorIs(t, err, layercache.ErrMiss)
	})
	var client *redis.Client
	_, err := New(client)
	require.Error(t, err)
	_, err = New(nil)
	require.Error(t, err)
}

// The fake makes both request precision and response latency observable without
// relying on the real scheduler between two separate time.Now calls.
type delayedSetClient struct {
	Client
	ttl time.Duration
}

func (c *delayedSetClient) Process(_ context.Context, cmd redis.Cmder) error {
	c.ttl = time.Duration(cmd.Args()[4].(int64)) * time.Millisecond
	time.Sleep(20 * time.Millisecond)
	cmd.(*redis.StatusCmd).SetVal("OK")
	return nil
}

func TestWriteReceiptArithmetic(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client := &delayedSetClient{}
		store, err := New(client)
		require.NoError(t, err)
		start := time.Now()
		deadline, err := store.Set(context.Background(), "key", nil, time.Second+999*time.Microsecond)
		require.NoError(t, err)
		require.Equal(t, time.Second, client.ttl)
		require.Equal(t, start.Add(999*time.Millisecond), deadline)
		require.Equal(t, 979*time.Millisecond, time.Until(deadline))
	})
}

func TestConditionalAndBatchPipeline(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), ContextTimeoutEnabled: true, MaxRetries: -1})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	store, err := New(client)
	require.NoError(t, err)
	ctx := context.Background()
	_, stored, err := store.SetIf(ctx, "key", []byte("old"), time.Minute, layercache.SetIfAbsent)
	require.NoError(t, err)
	require.True(t, stored)
	_, stored, err = store.SetIf(ctx, "key", []byte("bad"), time.Minute, layercache.SetIfAbsent)
	require.NoError(t, err)
	require.False(t, stored)
	value, err := server.Get("key")
	require.NoError(t, err)
	require.Equal(t, "old", value)
	_, stored, err = store.SetIf(ctx, "key", []byte("new"), time.Minute, layercache.SetIfPresent)
	require.NoError(t, err)
	require.True(t, stored)
	_, stored, err = store.SetIf(ctx, "absent", nil, time.Minute, layercache.SetIfPresent)
	require.NoError(t, err)
	require.False(t, stored)
	deadlines, err := store.MSet(ctx, map[string][]byte{"a": []byte("A"), "b": []byte("B")}, time.Minute)
	require.NoError(t, err)
	require.Len(t, deadlines, 2)
	entries, err := store.MGet(ctx, []string{"a", "b", "missing"})
	require.NoError(t, err)
	require.Len(t, entries, 2)
	require.Equal(t, []byte("A"), entries["a"].Data)
	require.NoError(t, server.Set("persistent", "bad"))
	entries, err = store.MGet(ctx, []string{"a", "persistent"})
	require.ErrorIs(t, err, layercache.ErrInvalidTTL)
	require.Contains(t, entries, "a")
}

func TestTypedBatchThroughRealAdapter(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), ContextTimeoutEnabled: true, MaxRetries: -1})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	remote, err := New(client)
	require.NoError(t, err)
	base, err := layercache.New(layercache.WithLocal(nil), layercache.WithRemote(remote), layercache.WithNamespace("batch"), layercache.WithNotFoundTTL(time.Minute))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, base.Close()) })
	c := layercache.NewT[int, string](base)
	calls := 0
	load := func(_ context.Context, ids []int) (map[int]string, error) {
		calls++
		return map[int]string{1: "A", 2: "B"}, nil
	}
	for range 2 {
		got, err := c.MGet(context.Background(), "user", []int{3, 2, 1, 2}, load)
		require.NoError(t, err)
		require.Equal(t, map[int]string{1: "A", 2: "B"}, got)
	}
	require.Equal(t, 1, calls)
}

func TestLeaseOwnershipRenewalAndExpiry(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), ContextTimeoutEnabled: true, MaxRetries: -1})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	store, err := New(client)
	require.NoError(t, err)
	ctx := context.Background()
	_, ok, err := store.AcquireLease(ctx, "lease", "owner-a", time.Second)
	require.NoError(t, err)
	require.True(t, ok)
	_, ok, err = store.AcquireLease(ctx, "lease", "owner-b", time.Second)
	require.NoError(t, err)
	require.False(t, ok)
	server.FastForward(500 * time.Millisecond)
	_, ok, err = store.AcquireLease(ctx, "lease", "owner-a", time.Second)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, time.Second, server.TTL("lease"))
	server.FastForward(time.Second)
	_, ok, err = store.AcquireLease(ctx, "lease", "owner-b", time.Second)
	require.NoError(t, err)
	require.True(t, ok)
	owner, err := server.Get("lease")
	require.NoError(t, err)
	require.Equal(t, "owner-b", owner)
	_, _, err = store.AcquireLease(ctx, "lease", "", time.Second)
	require.ErrorIs(t, err, layercache.ErrInvalidOptions)
}

// Consume a successful conditional response and report EOF instead. With a
// retry-enabled client, an ordinary Cmd would retry NX and return 0 on attempt 2.
type lostReplyConn struct {
	net.Conn
	armed    atomic.Bool
	injected *atomic.Bool
	attempts *atomic.Int64
}

func (c *lostReplyConn) Write(b []byte) (int, error) {
	if bytes.Contains(b, []byte(conditionalScript)) {
		c.attempts.Add(1)
		if c.injected.CompareAndSwap(false, true) {
			c.armed.Store(true)
		}
	}
	return c.Conn.Write(b)
}
func (c *lostReplyConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	if c.armed.Swap(false) && err == nil && n > 0 {
		return 0, io.EOF
	}
	return n, err
}
func TestLostConditionalReplyIsNotRetriedIntoRejection(t *testing.T) {
	server := miniredis.RunT(t)
	var injected atomic.Bool
	var attempts atomic.Int64
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), Protocol: 2, ContextTimeoutEnabled: true, MaxRetries: 3, MinRetryBackoff: time.Millisecond, MaxRetryBackoff: time.Millisecond, Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := (&net.Dialer{}).DialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		return &lostReplyConn{Conn: conn, injected: &injected, attempts: &attempts}, nil
	}})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	remote, err := New(client)
	require.NoError(t, err)
	base, err := layercache.New(layercache.WithLocal(nil), layercache.WithRemote(remote), layercache.WithNamespace("retry"), layercache.WithLoadTimeout(5*time.Second), layercache.WithRemoteTimeout(time.Second))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, base.Close()) })
	typed := layercache.NewTyped[string](base)
	entered, release := make(chan struct{}), make(chan struct{})
	old := make(chan error, 1)
	go func() {
		_, err := typed.GetOrLoad(context.Background(), "key", func(context.Context, string) (string, error) { close(entered); <-release; return "old", nil })
		old <- err
	}()
	<-entered
	setErr := base.Set(context.Background(), "key", layercache.Value("new"), layercache.SetNX(true))
	stored, readErr := server.Get("5:retry:key")
	close(release)
	oldErr := <-old
	require.EqualValues(t, 1, attempts.Load(), "[ablation:conditional_no_retry]")
	require.Error(t, setErr)
	require.NotErrorIs(t, setErr, layercache.ErrNotStored)
	require.NoError(t, readErr)
	require.Contains(t, stored, "new")
	require.ErrorIs(t, oldErr, layercache.ErrInvalidated)
	value, err := typed.Get(context.Background(), "key")
	require.NoError(t, err)
	require.Equal(t, "new", value)
	require.Equal(t, 3, client.Options().MaxRetries, "application client options must remain unchanged")
}

// Function adapters are valid Client implementations, including a typed nil.
type processFunc func(context.Context, redis.Cmder) error

func (f processFunc) Process(ctx context.Context, cmd redis.Cmder) error { return f(ctx, cmd) }

type processValue struct{ processFunc }

func TestNewRejectsTypedNilFunctionClient(t *testing.T) {
	var fn processFunc
	store, err := New(fn)
	require.Error(t, err, "a nil function client must fail construction rather than panic on first use")
	require.Nil(t, store)
}

func TestNewAcceptsFunctionAndValueClients(t *testing.T) {
	called := 0
	fn := processFunc(func(_ context.Context, cmd redis.Cmder) error {
		called++
		cmd.(*redis.StatusCmd).SetVal("OK")
		return nil
	})
	for _, client := range []Client{fn, processValue{fn}} {
		store, err := New(client)
		require.NoError(t, err)
		_, err = store.Set(context.Background(), "key", []byte("value"), time.Second)
		require.NoError(t, err)
	}
	require.Equal(t, 2, called)
}
