// Package ratelimit demonstrates a per-client request-rate window (token
// bucket keyed by ClientIP by default): steady callers pass, bursts past the
// window's request count are refused until the bucket refills.
package ratelimit

import (
	"time"

	"github.com/apus-run/gala/components/ginx/middlewares/activelimit/ratelimit"
	"github.com/gin-gonic/gin"
)

func New() *gin.Engine {
	r := gin.New()
	r.Use(ratelimit.NewRateLimit(time.Minute, 2).Build()) // 2 requests per client per minute
	r.GET("/search", func(c *gin.Context) { c.String(200, "results") })
	return r
}
