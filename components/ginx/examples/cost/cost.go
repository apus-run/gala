// Package cost demonstrates per-request latency logging around the rest of
// the chain; mount it after the access log so both see the same request.
// Package cost 演示记录其余链路中每个请求的耗时；应挂在访问日志之后，
// 让两者看到同一个请求。
package cost

import (
	middleware "github.com/apus-run/gala/components/ginx/middlewares/cost"
	"github.com/gin-gonic/gin"
)

// New builds the demo engine with the cost middleware | New 构建挂了耗时日志中间件的示例引擎.
func New() *gin.Engine {
	r := gin.New()
	r.Use(middleware.NewBuilder().Build())
	r.GET("/", func(c *gin.Context) { c.String(200, "ok") })
	return r
}
