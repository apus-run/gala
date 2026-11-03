// Package error demonstrates translating handler errors into HTTP responses:
// handlers attach typed errorsx errors to the context and stay clean of
// response writing; the middleware renders the last one with its code.
// Package error 演示将 handler 错误转换为 HTTP 响应：handler 把带类型的
// errorsx 错误附加到上下文上，自身不负责写出响应；中间件用其错误码渲染
// 最后一条错误。
package error

import (
	"net/http"

	middleware "github.com/apus-run/gala/components/ginx/middlewares/error"
	"github.com/apus-run/gala/pkg/errorsx"
	"github.com/gin-gonic/gin"
)

// New builds the demo engine with the error middleware | New 构建挂了错误渲染中间件的示例引擎.
func New() *gin.Engine {
	r := gin.New()
	r.Use(middleware.NewBuilder().Build())
	r.GET("/orders/:id", func(c *gin.Context) {
		if c.Param("id") != "42" {
			_ = c.Error(errorsx.InvalidParams("ORDER_NOT_FOUND").
				WithMessage("order does not exist").
				KV("order_id", c.Param("id")))
			return
		}
		c.JSON(http.StatusOK, gin.H{"id": "42"})
	})
	return r
}
