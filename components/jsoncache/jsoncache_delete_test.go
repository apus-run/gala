package jsoncache

import (
	"context"
	"testing"

	"github.com/redis/go-redis/v9"
)

type item struct{ N int }

// Delete 必须删掉 Save 写入的那个 key（带 prefix），而不是删裸 key。
func TestDeleteUsesPrefix(t *testing.T) {
	cli := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	ctx := context.Background()
	if err := cli.Ping(ctx).Err(); err != nil {
		t.Skipf("redis not available: %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })

	c := New[item]("jc-test:", cli)
	if err := c.Save(ctx, "k", &item{N: 7}); err != nil {
		t.Fatal(err)
	}
	if err := c.Delete(ctx, "k"); err != nil {
		t.Fatal(err)
	}
	if n, _ := cli.Exists(ctx, "jc-test:k").Result(); n != 0 {
		t.Fatal("Delete left the prefixed key behind")
	}
	// 无关的裸 key 不能被误删
	cli.Set(ctx, "k", "keep", 0)
	t.Cleanup(func() { cli.Del(ctx, "k") })
	_ = c.Delete(ctx, "k")
	if v, _ := cli.Get(ctx, "k").Result(); v != "keep" {
		t.Fatal("Delete removed an unrelated unprefixed key")
	}
}
