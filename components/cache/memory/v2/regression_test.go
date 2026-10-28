package memory_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/apus-run/gala/components/cache/internal/errs"
	"github.com/apus-run/gala/components/cache/memory/v2"
)

var rctx = context.Background()

// GetAny 曾经在每次命中时 panic（断言成了 *Item[V]）。
func TestGetAnyDoesNotPanic(t *testing.T) {
	c := memory.New[string, string]()
	defer c.Close()
	_ = c.Set(rctx, "k", "v", time.Minute)

	if v := c.GetAny(rctx, "k"); v.Error != nil || v.Value != "v" {
		t.Fatalf("GetAny(hit) = %#v", v.AnyValue)
	}
	if v := c.GetAny(rctx, "missing"); !errors.Is(v.Error, errs.ErrKeyNotExist) || v.Value != nil {
		t.Fatalf("GetAny(miss) = %#v", v.AnyValue)
	}
	_ = c.Set(rctx, "old", "v", time.Nanosecond)
	time.Sleep(2100 * time.Millisecond)
	if v := c.GetAny(rctx, "old"); !errors.Is(v.Error, errs.ErrItemExpired) || v.Value != nil {
		t.Fatalf("GetAny(expired) = %#v", v.AnyValue)
	}
	// Get 过期时返回零值，不把旧值交出去。
	if got, err := c.Get(rctx, "old"); !errors.Is(err, errs.ErrItemExpired) || got != "" {
		t.Fatalf("Get(expired) = %q, %v, want \"\", ErrItemExpired", got, err)
	}
}

func TestContainsExpiredNoRace(t *testing.T) {
	c := memory.New[string, int]()
	defer c.Close()
	_ = c.Set(rctx, "k", 1, time.Nanosecond)
	time.Sleep(2100 * time.Millisecond)

	var wg sync.WaitGroup
	for range 16 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if c.Contains(rctx, "k") {
				t.Error("Contains(expired) = true")
			}
		}()
		go func() { defer wg.Done(); _, _ = c.Get(rctx, "k") }()
	}
	wg.Wait()
}

func TestConnReturnsCopy(t *testing.T) {
	c := memory.New[string, int]()
	defer c.Close()
	_ = c.Set(rctx, "k", 1, 0)

	snap := c.Conn()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); snap["injected"] = memory.Item[int]{Val: 1} }()
	go func() { defer wg.Done(); _ = c.Set(rctx, "other", 1, 0) }()
	wg.Wait()

	if c.Contains(rctx, "injected") {
		t.Fatal("mutating the Conn snapshot leaked into the cache")
	}
}

func TestCloseIdempotent(t *testing.T) {
	c := memory.New[string, int]()
	done := make(chan struct{})
	go func() {
		_ = c.Close()
		_ = c.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close blocked")
	}
}

func TestNonPositiveGCIntervalIsIgnored(t *testing.T) {
	for _, d := range []time.Duration{0, -time.Second} {
		c := memory.New[string, int](memory.WithGCInterval[string, int](d))
		_ = c.Set(rctx, "k", 1, 0)
		time.Sleep(50 * time.Millisecond)
		if !c.Contains(rctx, "k") {
			t.Fatalf("WithGCInterval(%v) broke the cache", d)
		}
		_ = c.Close()
	}
}

func TestDataCopiesInput(t *testing.T) {
	src := map[string]memory.Item[int]{"a": {Val: 1}}
	c := memory.New[string, int](memory.Data(src))
	defer c.Close()

	src["b"] = memory.Item[int]{Val: 2}
	delete(src, "a")
	if c.Contains(rctx, "b") {
		t.Fatal("a key added to the caller's map leaked into the cache")
	}
	if v, err := c.Get(rctx, "a"); err != nil || v != 1 {
		t.Fatalf("a key deleted from the caller's map vanished from the cache: %v, %v", v, err)
	}
}

func TestDataNil(t *testing.T) {
	c := memory.New[string, int](memory.Data[string, int](nil))
	defer c.Close()
	if err := c.Set(rctx, "k", 1, 0); err != nil || !c.Contains(rctx, "k") {
		t.Fatalf("Data(nil) cache unusable: %v", err)
	}
}

func TestKeysEmptyIsNil(t *testing.T) {
	c := memory.New[string, int]()
	defer c.Close()
	if keys := c.Keys(rctx); keys != nil {
		t.Fatalf("Keys on an empty cache = %#v, want nil", keys)
	}
}
