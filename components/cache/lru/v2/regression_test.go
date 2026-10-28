package lru_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/apus-run/gala/components/cache/internal/errs"
	"github.com/apus-run/gala/components/cache/lru/v2"
)

var rctx = context.Background()

func TestGetAnyNoDeadlockUnderWriters(t *testing.T) {
	c := lru.New[string, string]()
	defer c.Close()
	_ = c.Set(rctx, "k", "v", 0)

	done := make(chan struct{})
	go func() {
		var wg sync.WaitGroup
		for range 64 {
			wg.Add(2)
			go func() {
				defer wg.Done()
				for range 200 {
					_ = c.GetAny(rctx, "k")
				}
			}()
			go func() {
				defer wg.Done()
				for range 200 {
					_ = c.Set(rctx, "k", "v", 0)
				}
			}()
		}
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("GetAny deadlocked with concurrent writers")
	}
}

func TestExpiredGetDoesNotDeleteFreshValue(t *testing.T) {
	c := lru.New[string, string]()
	defer c.Close()
	_ = c.Set(rctx, "k", "old", time.Nanosecond)
	time.Sleep(1500 * time.Millisecond)
	for round := range 100 {
		_, _ = c.Get(rctx, "k")
		_ = c.Set(rctx, "k", "new", time.Hour)
		time.Sleep(time.Millisecond)
		if v, err := c.Get(rctx, "k"); err != nil || v != "new" {
			t.Fatalf("round %d: fresh value lost: %q, %v", round, v, err)
		}
		_ = c.Set(rctx, "k", "old", time.Nanosecond)
		time.Sleep(1500 * time.Millisecond * time.Duration(boolInt(round%50 == 0)))
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func TestWithSizeIsEnforced(t *testing.T) {
	c := lru.New[string, int](lru.WithSize[string, int](2))
	defer c.Close()
	for _, k := range []string{"a", "b", "c", "d"} {
		_ = c.Set(rctx, k, 1, 0)
	}
	if n := len(c.Keys(rctx)); n != 2 {
		t.Fatalf("WithSize(2) holds %d keys, want 2", n)
	}
}

func TestCloseIdempotent(t *testing.T) {
	c := lru.New[string, int]()
	c.Close()
	c.Close()
}

func TestDataEmptyMap(t *testing.T) {
	c := lru.New[string, int](lru.Data(map[string]lru.Item[int]{}))
	defer c.Close()
	if err := c.Set(rctx, "k", 1, 0); err != nil || !c.Contains(rctx, "k") {
		t.Fatalf("Data(empty) cache unusable: %v", err)
	}
}

func TestSetRejectsZeroKey(t *testing.T) {
	c := lru.New[string, int]()
	defer c.Close()
	if err := c.Set(rctx, "", 1, 0); !errors.Is(err, errs.ErrSetKeyFailed) {
		t.Fatalf("Set(\"\") = %v, want ErrSetKeyFailed", err)
	}
}

func TestNonPositiveGCIntervalIsIgnored(t *testing.T) {
	for _, d := range []time.Duration{0, -time.Second} {
		c := lru.New[string, int](lru.WithGCInterval[string, int](d))
		_ = c.Set(rctx, "k", 1, 0)
		time.Sleep(50 * time.Millisecond)
		if !c.Contains(rctx, "k") {
			t.Fatalf("WithGCInterval(%v) broke the cache", d)
		}
		c.Close()
	}
}

func TestKeysEmptyIsNil(t *testing.T) {
	c := lru.New[string, int]()
	defer c.Close()
	if keys := c.Keys(rctx); keys != nil {
		t.Fatalf("Keys on an empty cache = %#v, want nil", keys)
	}
	_ = c.Set(rctx, "old", 1, time.Nanosecond)
	time.Sleep(1500 * time.Millisecond)
	if keys := c.Keys(rctx); keys != nil {
		t.Fatalf("Keys with only expired entries = %#v, want nil", keys)
	}
}

func TestDataCopiesInput(t *testing.T) {
	src := map[string]lru.Item[int]{"a": {Val: 1}}
	c := lru.New[string, int](lru.Data(src))
	defer c.Close()
	src["b"] = lru.Item[int]{Val: 2}
	if c.Contains(rctx, "b") {
		t.Fatal("a key added to the caller's map leaked into the cache")
	}
}
