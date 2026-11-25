// Package crawlerdetect demonstrates the crawler-detection middleware: only
// requests whose User-Agent says "crawler" and whose IP verifies by DNS pass.
// Package crawlerdetect 演示爬虫检测中间件：只有 User-Agent 自称爬虫、且 IP
// 能通过 DNS 校验的请求才会放行。
package crawlerdetect

import (
	"net/http"

	middleware "github.com/apus-run/gala/components/ginx/middlewares/crawlerdetect"
	"github.com/gin-gonic/gin"
)

// New builds the demo engine | New 构建示例引擎。
func New() *gin.Engine {
	r := gin.New()
	r.Use(middleware.NewBuilder().Build())
	r.GET("/", func(c *gin.Context) { c.String(http.StatusOK, "hello crawler") })
	return r
}
