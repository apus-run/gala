// Package cost logs each request's duration with the standard library
// logger | cost 用标准库日志记录每个请求的耗时.
//
// # Notes | 说明
//
// Output goes straight to log.Printf — no level, no sampling, no sink
// injection. Only URL.Path is logged, never the query string, so tokens in
// queries stay out of the logs. Use accesslog when structured logging is
// needed.
// 输出直接走 log.Printf——无级别、无采样、不可注入出口。只记 URL.Path、不记
// 查询串，查询串里的 token 不会进日志。需要结构化日志时用 accesslog。
//
// # Example | 示例
//
//	r.Use(cost.NewBuilder().Build())
package cost

import (
	"log"
	"time"

	"github.com/gin-gonic/gin"
)

// Builder assembles the cost-logging middleware | Builder 组装耗时日志中间件.
type Builder struct{}

// NewBuilder creates a Builder | NewBuilder 创建 Builder。
func NewBuilder() *Builder {
	return &Builder{}
}

// Build returns the middleware that logs each request's duration
// | Build 返回记录每个请求耗时的中间件。
func (b *Builder) Build() gin.HandlerFunc {
	// Use a function callback | 使用函数回调
	return func(c *gin.Context) {
		// Record the start time | 记录开始时间
		start := time.Now()

		// Log the path only: RequestURI carries the query string, whose
		// tokens and passwords would end up in logs.
		// 只记路径：RequestURI 带着查询串，里面的 token、password 会进日志
		path := c.Request.URL.Path
		log.Printf("api uri start: %v", path)
		// Run the business logic via next | 使用 next 执行具体的业务逻辑
		c.Next()

		// Record the end time | 记录结束时间
		end := time.Now()
		cost := end.Sub(start)
		log.Printf("api uri end: %v, cost: %v", path, cost.Seconds())
	}
}

// cost.NewBuilder().Build() | 耗时中间件用法示例
