package redis_gen

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func provision(t *testing.T, c *redis.Client, server int64) {
	t.Helper()
	exists, err := c.Exists(context.Background(), stateKey(server)).Result()
	if err != nil {
		t.Fatal(err)
	}
	if exists == 0 {
		if err := InitializeState(context.Background(), c, server, 0, 0); err != nil {
			t.Fatal(err)
		}
	}
}

func newTestRedis(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	m := miniredis.RunT(t)
	m.SetTime(time.Unix(1700000000, 0))
	c := redis.NewClient(&redis.Options{Addr: m.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = c.Close() })
	return c, m
}
func newGenerator(t *testing.T, c *redis.Client, server int64) *generator {
	t.Helper()
	provision(t, c, server)
	g, err := NewIDGenerator(c, []int64{server})
	if err != nil {
		t.Fatal(err)
	}
	return g.(*generator)
}
func split(n int64) (ms, seq, server int64) {
	return (n>>32)*1000 + ((n >> 22) & 1023), (n >> 14) & 255, n & 16383
}

func TestBatchAcrossSequenceAndSecondBoundaries(t *testing.T) {
	for _, count := range []int{0, 1, 255, 256, 257, 2048, MaxBatchSize} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			c, m := newTestRedis(t)
			m.SetTime(time.Unix(1700000000, 999000000))
			g := newGenerator(t, c, 16383)
			ids, err := g.GenMultiIDs(context.Background(), count)
			if err != nil || len(ids) != count {
				t.Fatalf("batch len=%d err=%v", len(ids), err)
			}
			for n, value := range ids {
				ms, seq, server := split(value)
				if value < 0 || ms != 1700000000999+int64(n)/256 || seq != int64(n)%256 || server != 16383 {
					t.Fatalf("invalid ID at %d: %d (%d %d %d)", n, value, ms, seq, server)
				}
				if n > 0 && value <= ids[n-1] {
					t.Fatal("batch did not increase")
				}
			}
			if count > 0 && m.TTL(stateKey(16383)) != 0 {
				t.Fatal("state must persist")
			}
		})
	}
}

func TestConcurrentGeneratorsSharingServerID(t *testing.T) {
	c, m := newTestRedis(t)
	shared := newGenerator(t, c, 7)
	const workers = 12
	results := make(chan []int64, workers)
	var wg sync.WaitGroup
	for worker := range workers {
		g := shared
		if worker%2 == 1 {
			independent := redis.NewClient(&redis.Options{Addr: m.Addr()})
			t.Cleanup(func() { _ = independent.Close() })
			g = newGenerator(t, independent, 7)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			var all []int64
			for n := 0; n < 30; n++ {
				ids, err := g.GenMultiIDs(context.Background(), 257)
				if err != nil {
					t.Error(err)
					return
				}
				all = append(all, ids...)
			}
			results <- all
		}()
	}
	wg.Wait()
	close(results)
	seen := map[int64]bool{}
	for ids := range results {
		for _, n := range ids {
			if seen[n] {
				t.Fatalf("duplicate %d", n)
			}
			seen[n] = true
		}
	}
	if len(seen) != workers*30*257 {
		t.Fatalf("missing IDs: %d", len(seen))
	}
}

func TestRollbackAndRestartKeepAllocationProgress(t *testing.T) {
	c, m := newTestRedis(t)
	g := newGenerator(t, c, 1)
	first, err := g.GenMultiIDs(context.Background(), 300)
	if err != nil {
		t.Fatal(err)
	}
	m.FastForward(24 * time.Hour)
	m.SetTime(time.Unix(1600000000, 0))
	nextClient := redis.NewClient(&redis.Options{Addr: m.Addr()})
	defer nextClient.Close()
	next := newGenerator(t, nextClient, 1)
	ids, err := next.GenMultiIDs(context.Background(), 300)
	if err != nil {
		t.Fatal(err)
	}
	if ids[0] <= first[len(first)-1] {
		t.Fatal("restart/rollback reused allocation")
	}
}

func TestAllocationStateFailsClosed(t *testing.T) {
	for _, name := range []string{"partial", "invalid-sequence", "negative-time", "expiring", "wrong-type", "exhausted"} {
		t.Run(name, func(t *testing.T) {
			c, m := newTestRedis(t)
			g := newGenerator(t, c, 1)
			key := stateKey(1)
			switch name {
			case "partial":
				m.HDel(key, "seq")
				m.HSet(key, "ms", "1700000000000")
			case "invalid-sequence":
				m.HSet(key, "ms", "1700000000000", "seq", "256")
			case "negative-time":
				m.HSet(key, "ms", "-1", "seq", "0")
			case "expiring":
				m.HSet(key, "ms", "1700000000000", "seq", "1")
				m.SetTTL(key, time.Minute)
			case "wrong-type":
				m.Del(key)
				m.Set(key, "not a hash")
			case "exhausted":
				m.HSet(key, "ms", "2147483647999", "seq", "255")
			}
			if ids, err := g.GenMultiIDs(context.Background(), 2); err == nil || ids != nil {
				t.Fatalf("state accepted: %v %v", ids, err)
			}
		})
	}
}

func TestTimestampExhaustionDoesNotModifyState(t *testing.T) {
	c, m := newTestRedis(t)
	g := newGenerator(t, c, 1)
	m.SetTime(time.Unix(2147483647, 999000000))
	if _, err := g.GenMultiIDs(context.Background(), 256); err != nil {
		t.Fatal(err)
	}
	before := m.HGet(stateKey(1), "seq")
	if _, err := g.GenID(context.Background()); err == nil {
		t.Fatal("accepted sign-bit overflow")
	}
	if m.HGet(stateKey(1), "seq") != before {
		t.Fatal("failed allocation changed state")
	}
}

func TestValidationCancellationAndNoFallback(t *testing.T) {
	c, m := newTestRedis(t)
	if _, err := NewIDGenerator(nil, []int64{1}); err == nil {
		t.Fatal("nil client accepted")
	}
	for _, ids := range [][]int64{nil, {-1}, {16384}} {
		if _, err := NewIDGenerator(c, ids); err == nil {
			t.Fatalf("accepted %v", ids)
		}
	}
	servers := []int64{1}
	provision(t, c, 1)
	base, err := NewIDGenerator(c, servers)
	if err != nil {
		t.Fatal(err)
	}
	servers[0] = -1
	for _, n := range []int{-1, MaxBatchSize + 1} {
		if _, err := base.GenMultiIDs(context.Background(), n); err == nil {
			t.Fatalf("accepted %d", n)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := base.GenID(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	if len(m.Keys()) != 1 || m.HGet(stateKey(1), "ms") != "0" {
		t.Fatal("invalid/canceled calls allocated state")
	}
	if _, err := base.GenID(context.Background()); err != nil {
		t.Fatal(err)
	}
	m.Close()
	if ids, err := base.GenMultiIDs(context.Background(), 10); err == nil || ids != nil {
		t.Fatalf("Redis failure returned IDs: %v %v", ids, err)
	}
}

// Model an ambiguous outcome: Redis committed the interval but its reply was
// lost. A retry must skip that interval, never recycle it.
type loseReply struct{ armed atomic.Bool }

func (h *loseReply) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h *loseReply) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}
func (h *loseReply) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		err := next(ctx, cmd)
		if err == nil && cmd.Name() == "eval" && h.armed.CompareAndSwap(true, false) {
			return context.DeadlineExceeded
		}
		return err
	}
}
func TestLostReplyConsumesIntervalWithoutReusingIt(t *testing.T) {
	c, _ := newTestRedis(t)
	hook := &loseReply{}
	c.AddHook(hook)
	g := newGenerator(t, c, 1)
	hook.armed.Store(true)
	if ids, err := g.GenMultiIDs(context.Background(), 256); !errors.Is(err, context.DeadlineExceeded) || ids != nil {
		t.Fatalf("ambiguous result returned IDs: %v %v", ids, err)
	}
	n, err := g.GenID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ms, seq, _ := split(n)
	if ms != 1700000000001 || seq != 0 {
		t.Fatalf("retry reused reserved interval: %d %d", ms, seq)
	}
}

func TestMissingRedisStateNeverAutoInitializes(t *testing.T) {
	c, m := newTestRedis(t)
	g := newGenerator(t, c, 1)
	if _, err := g.GenID(context.Background()); err != nil {
		t.Fatal(err)
	}
	m.Del(stateKey(1))
	// Reconstruct the generator to ensure the protection is not process-local.
	next, err := NewIDGenerator(c, []int64{1})
	if err != nil {
		t.Fatal(err)
	}
	if ids, err := next.GenMultiIDs(context.Background(), 1); !errors.Is(err, ErrStateMissing) || ids != nil {
		t.Fatalf("missing state generated IDs: %v %v", ids, err)
	}
	if m.Exists(stateKey(1)) {
		t.Fatal("generation silently recreated missing state")
	}
}

func TestInitializationDoesNotResetExistingProgress(t *testing.T) {
	c, m := newTestRedis(t)
	g := newGenerator(t, c, 1)
	first, err := g.GenID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	before := m.HGet(stateKey(1), "ms")
	if err := InitializeState(context.Background(), c, 1, 0, 0); err == nil {
		t.Fatal("initialization reset an existing partition")
	}
	if m.HGet(stateKey(1), "ms") != before {
		t.Fatal("initialization changed progress")
	}
	next, err := g.GenID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if next <= first {
		t.Fatal("reinitialization reused an ID")
	}
}
