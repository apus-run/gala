// Package redislimit demonstrates the cluster-wide active-request cap: leases
// live in Redis with a TTL, so N is shared across every process holding the
// same key, and a crashed process's leases expire instead of leaking.
package redislimit

import (
	"time"

	"github.com/apus-run/gala/components/ginx/middlewares/activelimit/redislimit"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// New mounts a limit of `max` concurrent requests on client cmd, shared by
// every process using the same Redis key.
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
