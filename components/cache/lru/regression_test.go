package lru_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/apus-run/gala/components/cache/internal/errs"
	"github.com/apus-run/gala/components/cache/lru"
)

var rctx = context.Background()

// GetAny 曾经先拿读锁再调 Get（又拿一次读锁）：有写者排队时会死锁。
func TestGetAnyNoDeadlockUnderWriters(t *testing.T) {
	c := lru.New()
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

// 读到过期项后立刻写入新值：新值不能被（曾经的）异步删除带走。
func TestExpiredGetDoesNotDeleteFreshValue(t *testing.T) {
	for round := range 100 {
		c := lru.New()
		_ = c.Set(rctx, "k", "old", time.Nanosecond)
		time.Sleep(1100 * time.Millisecond * time.Duration(boolInt(round == 0))) // 第一轮确保真的过期
		_, _ = c.Get(rctx, "k")
		_ = c.Set(rctx, "k", "new", time.Hour)
		time.Sleep(time.Millisecond)
		if v, err := c.Get(rctx, "k"); err != nil || v != "new" {
			t.Fatalf("round %d: fresh value lost: %v, %v", round, v, err)
		}
		c.Close()
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// WithSize 曾经只记数字，容量还是默认的 1000。
func TestWithSizeIsEnforced(t *testing.T) {
	c := lru.New(lru.WithSize(2))
	defer c.Close()
	for _, k := range []string{"a", "b", "c", "d"} {
		_ = c.Set(rctx, k, 1, 0)
	}
	if n := len(c.Keys(rctx)); n != 2 {
		t.Fatalf("WithSize(2) holds %d keys, want 2", n)
	}
	if !c.Contains(rctx, "d") || c.Contains(rctx, "a") {
		t.Fatal("LRU should keep the most recent keys")
	}
	// size<1 被忽略，不会把 cache 变得不可用。
	z := lru.New(lru.WithSize(0))
	defer z.Close()
	if err := z.Set(rctx, "k", 1, 0); err != nil || !z.Contains(rctx, "k") {
		t.Fatalf("WithSize(0) broke the cache: %v", err)
	}
}

// GetAny 的错误与 Get 一致：过期报 ErrItemExpired（曾经报 ErrKeyNotExist）。
func TestGetAnyErrorsMatchGet(t *testing.T) {
	c := lru.New()
	defer c.Close()
	_ = c.Set(rctx, "k", "v", 0)
	if v := c.GetAny(rctx, "k"); v.Error != nil || v.Value != "v" {
		t.Fatalf("GetAny(hit) = %#v", v.AnyValue)
	}
	if v := c.GetAny(rctx, "missing"); !errors.Is(v.Error, errs.ErrKeyNotExist) {
		t.Fatalf("GetAny(miss) err = %v", v.Error)
	}
	_ = c.Set(rctx, "old", "v", time.Nanosecond)
	time.Sleep(1100 * time.Millisecond)
	if v := c.GetAny(rctx, "old"); !errors.Is(v.Error, errs.ErrItemExpired) {
		t.Fatalf("GetAny(expired) err = %v, want ErrItemExpired", v.Error)
	}
}

// Close 可以重复调用：第二次曾经 panic（close of closed channel）。
func TestCloseIdempotent(t *testing.T) {
	c := lru.New()
	c.Close()
	c.Close()
}

// Data(空 map) 曾经 panic：lru.New(0) 返回 nil。
func TestDataEmptyMap(t *testing.T) {
	c := lru.New(lru.Data(map[string]lru.Item{}))
	defer c.Close()
	if err := c.Set(rctx, "k", 1, 0); err != nil || !c.Contains(rctx, "k") {
		t.Fatalf("Data(empty) cache unusable: %v", err)
	}
}

func TestSubSecondTTL(t *testing.T) {
	c := lru.New()
	defer c.Close()
	_ = c.Set(rctx, "k", "v", 500*time.Millisecond)
	if _, err := c.Get(rctx, "k"); err != nil {
		t.Fatalf("Get right after Set(500ms) = %v", err)
	}
}
