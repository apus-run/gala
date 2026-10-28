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
