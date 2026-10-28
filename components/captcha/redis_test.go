package captcha

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	rc "github.com/apus-run/gala/components/cache/redis"
)

// newRedisCaptcha 返回一个连到 GALA_CAPTCHA_REDIS_ADDR 的数字验证码实例；未设置时 skip。
func newRedisCaptcha(t *testing.T) *Captcha {
	t.Helper()
	addr := os.Getenv("GALA_CAPTCHA_REDIS_ADDR")
	if addr == "" {
		t.Skip("set GALA_CAPTCHA_REDIS_ADDR (e.g. 127.0.0.1:6379) to run")
	}
	cli := redis.NewClient(&redis.Options{Addr: addr, PoolSize: 64})
	t.Cleanup(func() { _ = cli.Close() })
	if err := cli.Ping(context.Background()).Err(); err != nil {
		t.Fatal(err)
	}
	return New(Digit, rc.New(cli), WithTTL(time.Minute))
}

// 多实例（各自的 redis 连接池）共用一个 redis：同一个验证码并发校验只能成功一次。
func TestRedisTwoInstancesReplay(t *testing.T) {
	a, b := newRedisCaptcha(t), newRedisCaptcha(t)
	for round := range 200 {
		id, _, answer, err := a.generate(ctx)
		if err != nil {
			t.Fatal(err)
		}
		n := verifyConcurrently(40, func(i int) bool {
			cp := a
			if i%2 == 1 {
				cp = b
			}
			ok, err := cp.Verify(ctx, id, answer)
			if err != nil {
				t.Error(err)
			}
			return ok
		})
		if n != 1 {
			t.Fatalf("round %d: one captcha verified %d times across two instances, want 1", round, n)
		}
	}
}

// 实例 a 出题、实例 b 校验；redis 上真的有 TTL，且校验后 key 被删除。
func TestRedisCrossInstanceAndTTL(t *testing.T) {
	a, b := newRedisCaptcha(t), newRedisCaptcha(t)
	id, _, answer, err := a.generate(ctx)
	if err != nil {
		t.Fatal(err)
	}

	cli := redis.NewClient(&redis.Options{Addr: os.Getenv("GALA_CAPTCHA_REDIS_ADDR")})
	defer cli.Close()
	ttl, err := cli.TTL(ctx, keyPrefix+id).Result()
	if err != nil || ttl <= 0 || ttl > time.Minute {
		t.Fatalf("redis TTL = %v, %v, want (0, 1m]", ttl, err)
	}

	if ok, err := b.Verify(ctx, id, " "+strings.ToUpper(answer)+" "); !ok || err != nil {
		t.Fatalf("cross-instance Verify = %v, %v", ok, err)
	}
	if n, _ := cli.Exists(ctx, keyPrefix+id).Result(); n != 0 {
		t.Fatal("key should be deleted after Verify")
	}
}
