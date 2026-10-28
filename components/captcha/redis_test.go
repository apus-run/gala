package captcha

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	rc "github.com/apus-run/gala/components/cache/redis"
)

// 多实例共用 redis 时，同一个验证码并发校验只能成功一次。
// 设置 GALA_CAPTCHA_REDIS_ADDR（如 127.0.0.1:6379）运行，否则 skip。
func TestRedisTwoInstancesReplay(t *testing.T) {
	addr := os.Getenv("GALA_CAPTCHA_REDIS_ADDR")
	if addr == "" {
		t.Skip("set GALA_CAPTCHA_REDIS_ADDR to run")
	}
	newStore := func() *cacheStore {
		cli := redis.NewClient(&redis.Options{Addr: addr, PoolSize: 64})
		t.Cleanup(func() { _ = cli.Close() })
		if err := cli.Ping(context.Background()).Err(); err != nil {
			t.Fatal(err)
		}
		return NewCacheStore(rc.New(cli), time.Minute).(*cacheStore)
	}
	a, b := newStore(), newStore()

	for round := range 200 {
		id := t.Name() + "-" + time.Now().Format("150405.000000000")
		if err := a.Set(id, "1234"); err != nil {
			t.Fatal(err)
		}
		n := verifyConcurrently(func(i int) bool {
			s := a
			if i%2 == 1 {
				s = b
			}
			return s.Verify(id, "1234", true)
		}, 40)
		if n != 1 {
			t.Fatalf("round %d: one captcha verified %d times across two instances, want 1", round, n)
		}
	}
}
