// Package requstid demonstrates mounting the request-ID middleware first in
// the chain: every downstream middleware and the client see the same ID, and
// an incoming X-Request-ID is honored instead of regenerated.
// Package requstid 演示将请求 ID 中间件挂在链首：每个下游中间件和客户端
// 看到的是同一个 ID，且已携带的 X-Request-ID 会被沿用而不是重新生成。
package requstid

import (
	"github.com/apus-run/gala/components/ginx/middlewares/requstid"
	"github.com/gin-gonic/gin"
)

// New builds the demo engine with the request-ID middleware mounted first | New 构建把请求 ID 中间件挂在链首的示例引擎.
func New() *gin.Engine {
	r := gin.New()
	r.Use(requstid.RequestID())
	r.GET("/whoami", func(c *gin.Context) {
		c.JSON(200, gin.H{"request_id": requstid.GetCtxRequestID(c)})
	})
	return r
}
