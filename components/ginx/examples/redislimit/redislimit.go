// Package redislimit demonstrates the cluster-wide active-request cap: leases
// live in Redis with a TTL, so N is shared across every process holding the
// same key, and a crashed process's leases expire instead of leaking.
// Package redislimit 演示集群级的并发请求上限：租约连同 TTL 存放在 Redis
// 中，N 由持有同一键的所有进程共享；崩溃进程的租约会过期，不会泄漏。
package redislimit

import (
	"time"

	"github.com/apus-run/gala/components/ginx/middlewares/activelimit/redislimit"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// New mounts a limit of `max` concurrent requests on client cmd, shared by
// every process using the same Redis key.
// New 在客户端 cmd 上挂载 `max` 个并发请求的上限，由使用同一 Redis 键的
// 所有进程共享。
func New(cmd redis.Cmdable, max int64) *gin.Engine {
	r := gin.New()
	r.Use(redislimit.NewRedisActiveLimit(cmd, max, "example:import:active").
		SetLeaseTTL(5 * time.Second).
		Build())
	r.POST("/import", func(c *gin.Context) {
		select {
		case <-time.After(50 * time.Millisecond):
			c.String(200, "imported")
		case <-c.Request.Context().Done():
			return
		}
	})
	return r
}
