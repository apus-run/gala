// Package metrics demonstrates Prometheus instrumentation: Metrics registers
// the /metrics scrape endpoint on the engine and records count, duration and
// sizes per request, with ignore options for paths that would skew them.
// Package metrics 演示 Prometheus 埋点：Metrics 在引擎上注册 /metrics
// 抓取端点，并记录每个请求的数量、耗时和大小，可用 ignore 选项排除会
// 干扰统计的路径。
package metrics

import (
	"net/http"

	"github.com/apus-run/gala/components/ginx/middlewares/metrics"
	"github.com/gin-gonic/gin"
)

// New builds the demo engine with the metrics middleware | New 构建挂了 Prometheus 埋点中间件的示例引擎.
func New() *gin.Engine {
	r := gin.New()
	r.Use(metrics.Metrics(r)) // registers GET /metrics on r itself | 在 r 自身注册 GET /metrics
	r.GET("/ping", func(c *gin.Context) { c.String(http.StatusOK, "pong") })
	return r
}
