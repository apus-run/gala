// Package ratelimit demonstrates a per-client request-rate window (token
// bucket keyed by ClientIP by default): steady callers pass, bursts past the
// window's request count are refused until the bucket refills.
// Package ratelimit 演示按客户端的请求速率窗口（默认以 ClientIP 为键的
// 令牌桶）：稳定的调用方正常通过，超过窗口请求数的突发流量会被拒绝，
// 直到令牌桶重新填满。
package ratelimit

import (
	"time"

	"github.com/apus-run/gala/components/ginx/middlewares/activelimit/ratelimit"
	"github.com/gin-gonic/gin"
)

// New builds the demo engine with the rate-limit middleware | New 构建挂了限流中间件的示例引擎.
func New() *gin.Engine {
	r := gin.New()
	r.Use(ratelimit.NewRateLimit(time.Minute, 2).Build()) // 2 requests per client per minute | 每客户端每分钟 2 个请求
	r.GET("/search", func(c *gin.Context) { c.String(200, "results") })
	return r
}
