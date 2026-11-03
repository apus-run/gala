// Package tracing demonstrates OpenTelemetry span creation per request: with
// no provider configured it uses the global (noop) one, so this wiring is safe
// in tests and activates itself once the process registers a real tracer.
// Package tracing 演示为每个请求创建 OpenTelemetry span：未配置 provider
// 时使用全局（noop）实现，因此这套接线在测试中是安全的，进程注册真实
// tracer 后会自动生效。
package tracing

import (
	"net/http"

	"github.com/apus-run/gala/components/ginx/middlewares/tracing"
	"github.com/gin-gonic/gin"
)

// New builds the demo engine with the tracing middleware | New 构建挂了链路追踪中间件的示例引擎.
func New() *gin.Engine {
	r := gin.New()
	r.Use(tracing.Tracing("order-service"))
	r.GET("/orders", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"orders": []int{1}}) })
	return r
}
