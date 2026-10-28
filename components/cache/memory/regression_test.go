package memory_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/apus-run/gala/components/cache/internal/errs"
	"github.com/apus-run/gala/components/cache/memory"
)

var rctx = context.Background()

// GetAny 返回存入的值本身，而不是内部的 Item；未命中/过期带上对应错误。
func TestGetAnyReturnsStoredValue(t *testing.T) {
	c := memory.New()
	defer c.Close()
	_ = c.Set(rctx, "k", "v", time.Minute)

	if v := c.GetAny(rctx, "k"); v.Error != nil || v.Value != "v" {
		t.Fatalf("GetAny(hit) = %#v, want Value \"v\"", v.AnyValue)
	}
	if v := c.GetAny(rctx, "missing"); !errors.Is(v.Error, errs.ErrKeyNotExist) || v.Value != nil {
		t.Fatalf("GetAny(miss) = %#v", v.AnyValue)
	}
	if v := c.GetAny(rctx, ""); !errors.Is(v.Error, errs.ErrKeyNotExist) {
		t.Fatalf("GetAny(\"\") = %#v", v.AnyValue)
	}
	_ = c.Set(rctx, "old", "v", time.Nanosecond)
	time.Sleep(2100 * time.Millisecond)
	if v := c.GetAny(rctx, "old"); !errors.Is(v.Error, errs.ErrItemExpired) || v.Value != nil {
		t.Fatalf("GetAny(expired) = %#v", v.AnyValue)
	}
}

// Contains 对过期 key 返回 false，并发调用没有数据竞争（配合 -race）。
func TestContainsExpiredNoRace(t *testing.T) {
	c := memory.New()
	defer c.Close()
	_ = c.Set(rctx, "k", "v", time.Nanosecond)
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

// Conn 返回快照：修改它不影响缓存，也不和并发写构成数据竞争。
func TestConnReturnsCopy(t *testing.T) {
	c := memory.New()
	defer c.Close()
	_ = c.Set(rctx, "k", "v", 0)

	snap := c.Conn()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); snap["injected"] = memory.Item{Val: 1} }()
	go func() { defer wg.Done(); _ = c.Set(rctx, "other", 1, 0) }()
	wg.Wait()

	if c.Contains(rctx, "injected") {
		t.Fatal("mutating the Conn snapshot leaked into the cache")
	}
	if _, ok := snap["k"]; !ok {
		t.Fatal("snapshot is missing existing data")
	}
}

// Close 可以重复调用：第二次曾经永远阻塞。
func TestCloseIdempotent(t *testing.T) {
	c := memory.New()
	done := make(chan struct{})
	go func() {
		_ = c.Close()
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

// 亚秒 TTL 不会写入即过期，并且至少活满 TTL。
func TestSubSecondTTL(t *testing.T) {
	c := memory.New()
	defer c.Close()
	_ = c.Set(rctx, "k", "v", 500*time.Millisecond)
	if _, err := c.Get(rctx, "k"); err != nil {
		t.Fatalf("Get right after Set(500ms) = %v", err)
	}
	time.Sleep(400 * time.Millisecond)
	if _, err := c.Get(rctx, "k"); err != nil {
		t.Fatalf("Get after 400ms of a 500ms TTL = %v", err)
	}
}

// 空 key 或 nil 值写入时报错，而不是假装成功。
func TestSetRejectsEmptyKeyAndNil(t *testing.T) {
	c := memory.New()
	defer c.Close()
	if err := c.Set(rctx, "", "v", 0); !errors.Is(err, errs.ErrSetKeyFailed) {
		t.Fatalf("Set(\"\") = %v, want ErrSetKeyFailed", err)
	}
	if err := c.Set(rctx, "k", nil, 0); !errors.Is(err, errs.ErrSetKeyFailed) {
		t.Fatalf("Set(nil) = %v, want ErrSetKeyFailed", err)
	}
	if c.Contains(rctx, "k") {
		t.Fatal("rejected Set must not store anything")
	}
}

func TestNonPositiveGCIntervalIsIgnored(t *testing.T) {
	for _, d := range []time.Duration{0, -time.Second} {
		c := memory.New(memory.WithGCInterval(d))
		_ = c.Set(rctx, "k", 1, 0)
		time.Sleep(50 * time.Millisecond)
		if !c.Contains(rctx, "k") {
			t.Fatalf("WithGCInterval(%v) broke the cache", d)
		}
		_ = c.Close()
	}
}
