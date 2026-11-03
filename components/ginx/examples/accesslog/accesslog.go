// Package accesslog demonstrates structured per-request logging: one entry per
// finished request with method, path, status, duration and - when allowed -
// request and response bodies, truncated to MaxLength.
// Package accesslog 演示结构化的按请求日志：每个请求结束后生成一条记录，
// 包含方法、路径、状态码、耗时，以及在允许时的请求和响应体（截断到
// MaxLength）。
package accesslog

import (
	"context"
	"log"

	"github.com/apus-run/gala/components/ginx/middlewares/accesslog"
	"github.com/gin-gonic/gin"
)

// New builds the demo engine with the access-log middleware | New 构建挂了访问日志中间件的示例引擎.
func New(sink func(context.Context, *accesslog.AccessLog)) *gin.Engine {
	r := gin.New()
	r.Use(accesslog.NewBuilder(sink).
		AllowReqBody().
		AllowRespBody().
		MaxLength(1024).
		IgnoreRoutes("/healthz").
		Build())
	r.GET("/echo", func(c *gin.Context) { c.JSON(200, gin.H{"msg": "hi"}) })
	r.GET("/healthz", func(c *gin.Context) { c.Status(200) })
	return r
}

// Stdout is the simplest production sink: JSON to the default logger.
// Stdout 是最简单的生产用接收端：以 JSON 输出到默认日志器。
func Stdout() func(context.Context, *accesslog.AccessLog) {
	return func(_ context.Context, al *accesslog.AccessLog) { log.Printf("%+v", *al) }
}
