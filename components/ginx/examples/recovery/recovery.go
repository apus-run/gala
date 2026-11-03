// Package recovery demonstrates the panic safety net: a panicking handler
// becomes a 500 JSON response instead of a dropped connection. Mount it early
// so access logs still record the request.
// Package recovery 演示 panic 安全网：发生 panic 的 handler 会返回 500
// JSON 响应，而不是连接被直接断开。应尽早挂载，使访问日志仍能记录该请求。
package recovery

import (
	"github.com/apus-run/gala/components/ginx/middlewares/recovery"
	"github.com/gin-gonic/gin"
)

// New builds the demo engine with the recovery middleware | New 构建挂了 panic 恢复中间件的示例引擎.
func New() *gin.Engine {
	r := gin.New()
	r.Use(recovery.NewBuilder().Build())
	r.GET("/orders/:id", func(c *gin.Context) {
		if c.Param("id") == "0" { // a nil-map write or similar programming error | 类似向 nil map 写入的编程错误
			panic("order index not initialized")
		}
		c.JSON(200, gin.H{"id": c.Param("id")})
	})
	return r
}
