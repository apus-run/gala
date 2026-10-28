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

// 亚秒 TTL 不会写入即过期（向下取整时会）。TTL 只有秒级精度：寿命在 (⌈ttl⌉-1s, ⌈ttl⌉]
// 之间浮动，所以只能断言「刚写入就能读到」和「过了 ⌈ttl⌉ 一定过期」，
// 不能断言「活满 ttl」。
func TestSubSecondTTL(t *testing.T) {
	c := memory.New()
	defer c.Close()
	_ = c.Set(rctx, "k", "v", 500*time.Millisecond)
	if _, err := c.Get(rctx, "k"); err != nil {
		t.Fatalf("Get right after Set(500ms) = %v", err)
	}
	time.Sleep(1600 * time.Millisecond) // ⌈500ms⌉ = 1s，留足调度余量
	if _, err := c.Get(rctx, "k"); !errors.Is(err, errs.ErrItemExpired) {
		t.Fatalf("Get after 1.6s of a 500ms TTL = %v, want ErrItemExpired", err)
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

// Data 复制传入的 map：调用方事后修改自己的 map，不能影响（也不能绕过锁地修改）缓存。
func TestDataCopiesInput(t *testing.T) {
	src := map[string]memory.Item{"a": {Val: 1}}
	c := memory.New(memory.Data(src))
	defer c.Close()

	src["b"] = memory.Item{Val: 2}
	delete(src, "a")
	if c.Contains(rctx, "b") {
		t.Fatal("a key added to the caller's map leaked into the cache")
	}
	if v, err := c.Get(rctx, "a"); err != nil || v != 1 {
		t.Fatalf("a key deleted from the caller's map vanished from the cache: %v, %v", v, err)
	}
}

// Data(nil) 曾经让第一次 Set 因写 nil map 而 panic。
func TestDataNil(t *testing.T) {
	c := memory.New(memory.Data(nil))
	defer c.Close()
	if err := c.Set(rctx, "k", 1, 0); err != nil || !c.Contains(rctx, "k") {
		t.Fatalf("Data(nil) cache unusable: %v", err)
	}
}

// Keys 没有有效 key 时返回 nil（与 lru、redis 一致）；过期的 key 不算。
func TestKeysEmptyIsNil(t *testing.T) {
	c := memory.New()
	defer c.Close()
	if keys := c.Keys(rctx); keys != nil {
		t.Fatalf("Keys on an empty cache = %#v, want nil", keys)
	}
	_ = c.Set(rctx, "k", 1, 0)
	if keys := c.Keys(rctx); len(keys) != 1 {
		t.Fatalf("Keys = %v", keys)
	}
}

// Delete 与 Deletes 对不存在的 key 不报错；Deletes 返回实际删除的个数。
func TestDeleteMissingAndCount(t *testing.T) {
	c := memory.New()
	defer c.Close()
	_ = c.Set(rctx, "a", 1, 0)
	if err := c.Delete(rctx, "nope"); err != nil {
		t.Fatalf("Delete(missing) = %v", err)
	}
	if n, err := c.Deletes(rctx); n != 0 || err != nil {
		t.Fatalf("Deletes() = %d, %v", n, err)
	}
	if n, err := c.Deletes(rctx, "a", "a", "nope"); n != 1 || err != nil {
		t.Fatalf("Deletes(a, a, nope) = %d, %v, want 1", n, err)
	}
}
