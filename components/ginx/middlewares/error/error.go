// Package error maps the last errorsx error attached to the gin context into
// a JSON response | error 把 gin context 上最后一条 errorsx 错误映射为 JSON 响应.
//
// # Notes | 说明
//
// It runs after c.Next and stays silent when the response is already
// written or no errorsx-shaped error is found in c.Errors. Codes outside
// 100-599 are rewritten to 500 before writing.
// 它在 c.Next 之后运行：响应已写出或 c.Errors 里找不到 errorsx 形态的错误时
// 不做任何事。状态码不在 100-599 区间时先改写为 500 再写响应。
//
// # Example | 示例
//
//	r.Use(error.NewBuilder().Build())
package error

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/apus-run/gala/pkg/errorsx"
)

// Builder assembles the error-mapping middleware | Builder 组装错误映射中间件.
type Builder struct{}

// NewBuilder creates a Builder | NewBuilder 创建 Builder。
func NewBuilder() *Builder {
	return &Builder{}
}

// Build returns the middleware that writes the last errorsx error as JSON
// | Build 返回把最后一条 errorsx 错误写成 JSON 的中间件。
func (b *Builder) Build() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		// Skip when downstream already wrote the response.
		// 下游已经写回响应时，避免重复写入。
		if c.Writer.Written() {
			return
		}

		if len(c.Errors) == 0 {
			return
		}

		appErr := errorsx.FromError(c.Errors.Last().Err).Clone()
		if appErr == nil {
			return
		}

		if appErr.Code < 100 || appErr.Code > 599 {
			appErr.Code = http.StatusInternalServerError
		}

		resp := gin.H{
			"code":    appErr.Code,
			"status":  appErr.Status,
			"message": appErr.Message,
		}
		if appErr.Details != nil {
			resp["details"] = appErr.Details
		}

		c.AbortWithStatusJSON(appErr.Code, resp)
	}
}
