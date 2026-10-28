package redis_test

import (
	"context"
	"os"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	r "github.com/apus-run/gala/components/cache/redis"
)

// Keys 改用 SCAN：大于单页（1000）的 key 也能全部遍历到。
// 需要 GALA_CACHE_REDIS_ADDR，否则 skip；用 db 15，测试前后清空。
func TestKeysScansAllPages(t *testing.T) {
	addr := os.Getenv("GALA_CACHE_REDIS_ADDR")
	if addr == "" {
		t.Skip("set GALA_CACHE_REDIS_ADDR to run")
	}
	ctx := context.Background()
	cli := redis.NewClient(&redis.Options{Addr: addr, DB: 15})
	t.Cleanup(func() { cli.FlushDB(ctx); _ = cli.Close() })
	if err := cli.FlushDB(ctx).Err(); err != nil {
		t.Fatal(err)
	}

	c := r.New(cli)
	const n = 2500
	pipe := cli.Pipeline()
	for i := range n {
		pipe.Set(ctx, "k"+strconv.Itoa(i), i, 0)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		t.Fatal(err)
	}

	keys := c.Keys(ctx)
	slices.Sort(keys)
	keys = slices.Compact(keys) // SCAN 可能重复返回同一个 key
	if len(keys) != n {
		t.Fatalf("Keys returned %d unique keys, want %d", len(keys), n)
	}
}

func realRedis(t *testing.T) (*r.Cache, *redis.Client) {
	t.Helper()
	addr := os.Getenv("GALA_CACHE_REDIS_ADDR")
	if addr == "" {
		t.Skip("set GALA_CACHE_REDIS_ADDR to run")
	}
	ctx := context.Background()
	cli := redis.NewClient(&redis.Options{Addr: addr, DB: 15})
	t.Cleanup(func() { cli.FlushDB(ctx); _ = cli.Close() })
	if err := cli.FlushDB(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	return r.New(cli), cli
}

// DEL 不带 key 会被 redis 拒绝；Deletes() 与 memory、lru 一致，返回 0。
func TestDeletesNoKeys(t *testing.T) {
	c, _ := realRedis(t)
	if n, err := c.Deletes(context.Background()); n != 0 || err != nil {
		t.Fatalf("Deletes() = %d, %v, want 0, nil", n, err)
	}
}

// Delete/Deletes 对不存在的 key 不报错；Deletes 返回实际删除的个数，并发删除同一个 key
// 只有一个调用得到 1（captcha 的一次性校验依赖这一点）。
func TestDeleteSemanticsAndAtomicCount(t *testing.T) {
	ctx := context.Background()
	c, _ := realRedis(t)
	if err := c.Delete(ctx, "nope"); err != nil {
		t.Fatalf("Delete(missing) = %v", err)
	}
	_ = c.Set(ctx, "a", "1", time.Minute)
	if n, err := c.Deletes(ctx, "a", "nope"); n != 1 || err != nil {
		t.Fatalf("Deletes(a, nope) = %d, %v, want 1", n, err)
	}

	for round := range 50 {
		key := "once-" + strconv.Itoa(round)
		_ = c.Set(ctx, key, "v", time.Minute)
		var wins atomic.Int32
		var wg sync.WaitGroup
		for range 16 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if n, _ := c.Deletes(ctx, key); n == 1 {
					wins.Add(1)
				}
			}()
		}
		wg.Wait()
		if wins.Load() != 1 {
			t.Fatalf("round %d: %d callers deleted the same key, want 1", round, wins.Load())
		}
	}
}

// Cache 接口文档里写的 redis 值类型约定，在真实服务器上逐条验证。
func TestValueTypeContract(t *testing.T) {
	ctx := context.Background()
	c, _ := realRedis(t)

	for name, tc := range map[string]struct {
		val  any
		want any
	}{
		"string": {"s", "s"},
		"int":    {42, "42"},
		"bool":   {true, "1"},
		"bytes":  {[]byte("b"), "b"},
	} {
		if err := c.Set(ctx, "k-"+name, tc.val, time.Minute); err != nil {
			t.Fatalf("Set(%s) = %v", name, err)
		}
		if got, err := c.Get(ctx, "k-"+name); err != nil || got != tc.want {
			t.Fatalf("Get(%s) = %#v, %v, want %#v", name, got, err, tc.want)
		}
	}
	type S struct{ A int }
	for name, v := range map[string]any{"struct": S{1}, "map": map[string]int{"a": 1}, "slice": []int{1}} {
		if err := c.Set(ctx, "k-"+name, v, time.Minute); err == nil {
			t.Fatalf("Set(%s) should fail: redis does not serialize", name)
		}
		if c.Contains(ctx, "k-"+name) {
			t.Fatalf("a failed Set(%s) stored something", name)
		}
	}
}

// Keys 没有 key 时返回 nil（与 memory、lru 一致）。
func TestKeysEmptyIsNil(t *testing.T) {
	c, _ := realRedis(t)
	if keys := c.Keys(context.Background()); keys != nil {
		t.Fatalf("Keys on an empty db = %#v, want nil", keys)
	}
}
