// Package cors demonstrates browser access control for one trusted origin:
// preflights are answered before any business middleware runs, and only the
// allowed origin and methods are echoed back.
// Package cors 演示针对单个可信来源的浏览器访问控制：预检请求在任何业务
// 中间件运行之前就被应答，且只回显被允许的来源和方法。
package cors

import (
	"net/http"

	"github.com/apus-run/gala/components/ginx/middlewares/cors"
	"github.com/gin-gonic/gin"
)

// New builds the demo engine with the CORS middleware | New 构建挂了 CORS 中间件的示例引擎.
func New() *gin.Engine {
	r := gin.New()
	r.Use(cors.NewCORS(
		cors.WithAllowOrigins([]string{"https://app.example.com"}),
		cors.WithAllowMethods([]string{http.MethodGet, http.MethodPost}),
		cors.WithAllowHeaders([]string{"Authorization", "Content-Type"}),
		cors.WithAllowCredentials(true),
		cors.WithMaxAge(12*3600),
	).Build())
	r.POST("/orders", func(c *gin.Context) { c.JSON(200, gin.H{"created": true}) })
	return r
}
