package ratelimit

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestLocalLimiterAllowsWithinBudget(t *testing.T) {
	l := New(nil, "test:", time.Minute, "inst")
	ctx := context.Background()
	const max = 3
	for i := 0; i < max; i++ {
		if !l.Allow(ctx, "k1", max) {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
	if l.Allow(ctx, "k1", max) {
		t.Fatal("request over budget should be denied")
	}
}

func TestRedisLimiterCountsRequestsInSameMillisecond(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	l := New(client, "test:", time.Minute, "inst")
	fixedNow := time.UnixMilli(1_700_000_000_000)
	l.now = func() time.Time { return fixedNow }

	const max = 3
	for i := 0; i < max; i++ {
		if !l.Allow(context.Background(), "same-ms", max) {
			t.Fatalf("request %d in the same millisecond should be allowed", i+1)
		}
	}
	if l.Allow(context.Background(), "same-ms", max) {
		t.Fatal("request over budget should be denied")
	}

	count, err := client.ZCard(context.Background(), "test:same-ms").Result()
	if err != nil {
		t.Fatalf("read Redis count: %v", err)
	}
	if count != max {
		t.Fatalf("Redis count = %d, want %d", count, max)
	}
}

func TestRedisLimitersWithSameInstanceIDDoNotCollide(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	fixedNow := time.UnixMilli(1_700_000_000_000)
	first := New(client, "test:", time.Minute, "shared-instance")
	second := New(client, "test:", time.Minute, "shared-instance")
	first.now = func() time.Time { return fixedNow }
	second.now = func() time.Time { return fixedNow }

	if !first.Allow(context.Background(), "key", 2) {
		t.Fatal("first limiter request should be allowed")
	}
	if !second.Allow(context.Background(), "key", 2) {
		t.Fatal("second limiter request should be allowed")
	}
	if first.Allow(context.Background(), "key", 2) {
		t.Fatal("request over the shared budget should be denied")
	}
}

func TestLocalLimiterIndependentKeys(t *testing.T) {
	l := New(nil, "test:", time.Minute, "inst")
	ctx := context.Background()
	if !l.Allow(ctx, "a", 1) {
		t.Fatal("first key should be allowed")
	}
	if !l.Allow(ctx, "b", 1) {
		t.Fatal("second key should be allowed")
	}
}

func TestAllowSkipsWhenMaxZero(t *testing.T) {
	l := New(nil, "test:", time.Minute, "inst")
	for i := 0; i < 100; i++ {
		if !l.Allow(context.Background(), "k", 0) {
			t.Fatal("max<=0 should always allow")
		}
	}
}

func TestNewClampsSubMillisecondWindow(t *testing.T) {
	l := New(nil, "test:", time.Nanosecond, "inst")
	if l.window != time.Millisecond {
		t.Fatalf("window = %v, want %v", l.window, time.Millisecond)
	}
}

func TestLocalLimiterExpiresOldRequests(t *testing.T) {
	l := newLocalLimiter()
	start := time.Unix(1_700_000_000, 0)

	if !l.allow("key", time.Minute, 1, start) {
		t.Fatal("first request should be allowed")
	}
	if l.allow("key", time.Minute, 1, start.Add(time.Minute-time.Nanosecond)) {
		t.Fatal("request inside the window should be denied")
	}
	if !l.allow("key", time.Minute, 1, start.Add(time.Minute)) {
		t.Fatal("request at the expired boundary should be allowed")
	}
}

func TestLocalCleanupKeepsActiveEntriesAndDeletesExpiredEntries(t *testing.T) {
	l := newLocalLimiter()
	now := time.Unix(1_700_000_000, 0)
	l.entries.Store("expired", &localEntry{timestamps: []time.Time{now.Add(-2 * time.Minute)}})
	l.entries.Store("active", &localEntry{timestamps: []time.Time{now.Add(-time.Second)}})

	l.cleanupExpired(time.Minute, now)

	if _, ok := l.entries.Load("expired"); ok {
		t.Fatal("expired entry should be deleted")
	}
	if _, ok := l.entries.Load("active"); !ok {
		t.Fatal("active entry should be kept")
	}
}

func TestStartCleanupStops(t *testing.T) {
	l := New(nil, "test:", time.Minute, "inst")
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		l.StartCleanup(stop)
		close(done)
	}()

	close(stop)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cleanup did not stop")
	}
}

func TestLocalLimiterConcurrentAllowAndCleanup(t *testing.T) {
	l := newLocalLimiter()
	now := time.Unix(1_700_000_000, 0)
	const workers = 32

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				l.allow("key", time.Minute, workers*100, now)
				l.cleanupExpired(time.Minute, now)
			}
		}()
	}
	wg.Wait()

	value, ok := l.entries.Load("key")
	if !ok {
		t.Fatal("active entry was lost during cleanup")
	}
	entry := value.(*localEntry)
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if got, want := len(entry.timestamps), workers*100; got != want {
		t.Fatalf("recorded requests = %d, want %d", got, want)
	}
}
