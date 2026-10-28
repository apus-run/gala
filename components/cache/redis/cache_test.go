package redis_test

import (
	"context"
	"os"
	"slices"
	"strconv"
	"testing"

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
