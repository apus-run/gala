// Package gzip demonstrates response compression for clients that ask for it
// (Accept-Encoding: gzip); requests without it pass through untouched.
// Package gzip 演示对声明支持的客户端（Accept-Encoding: gzip）压缩响应；
// 未声明的请求原样通过。
package gzip

import (
	"strings"

	middleware "github.com/apus-run/gala/components/ginx/middlewares/gzip"
	"github.com/gin-gonic/gin"
)

// New builds the demo engine with the gzip middleware | New 构建挂了 gzip 压缩中间件的示例引擎.
func New() *gin.Engine {
	r := gin.New()
	r.Use(middleware.NewBuilder().Build())
	r.GET("/report", func(c *gin.Context) {
		c.String(200, strings.Repeat("gala ", 2048)) // a payload worth compressing | 一个值得压缩的载荷
	})
	return r
}
