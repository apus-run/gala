// Package requstid injects an X-Request-ID into every gin request |
// requstid 为每个 gin 请求注入 X-Request-ID.
//
// # Notes | 说明
//
// An incoming X-Request-ID header is reused as-is; otherwise a uuid v4 is
// generated. The id is exposed three ways: the response header,
// gin.Context via [GetCtxRequestID], and request context via [CtxRequestID].
// 入站的 X-Request-ID 原样复用；缺失时生成 uuid v4。id 以三种方式暴露：
// 响应头、经 [GetCtxRequestID] 取的 gin.Context，以及经 [CtxRequestID] 取的
// 请求 context。
//
// # Example | 示例
//
//	r.Use(requstid.RequestID())
package requstid

import (
	"context"
	"uuid"

	"github.com/gin-gonic/gin"
)

const (
	// ContextRequestIDKey context request id for context | context 中的请求 id 键
	ContextRequestIDKey = "request_id"

	// HeaderXRequestIDKey http header request id key | HTTP 请求头中的请求 id 键
	HeaderXRequestIDKey = "X-Request-ID"
)

// CtxKeyString for context.WithValue key type | CtxKeyString 是 context.WithValue 的键类型.
type CtxKeyString string

// RequestIDKey "request_id" | RequestIDKey 即 "request_id".
var RequestIDKey = CtxKeyString(ContextRequestIDKey)

// RequestID is an interceptor that injects a 'X-Request-ID' into the context and request/response header of each request.
// RequestID 是一个拦截器：为每个请求注入 'X-Request-ID' 到 context 与请求/响应头。
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Check for incoming header, use it if exists
		// 检查入站头，存在则复用
		requestID := c.GetHeader(HeaderXRequestIDKey)

		// Create request id | 否则生成新的请求 id
		if requestID == "" {
			requestID = uuid.New().String() // assign, not shadow: the response header below must carry it | 赋值而非遮蔽：下方响应头必须携带它
			c.Request.Header.Set(HeaderXRequestIDKey, requestID)
		}

		// Expose it for use in the application, honored or generated alike
		// 暴露给应用使用，无论复用还是新生成
		c.Set(ContextRequestIDKey, requestID)

		// Set X-Request-ID header | 设置 X-Request-ID 响应头
		c.Writer.Header().Set(HeaderXRequestIDKey, requestID)

		c.Next()
	}
}

// GetCtxRequestID get request id from gin.Context
// GetCtxRequestID 从 gin.Context 取请求 id。
func GetCtxRequestID(c *gin.Context) string {
	if v, isExist := c.Get(ContextRequestIDKey); isExist {
		if requestID, ok := v.(string); ok {
			return requestID
		}
	}
	return ""
}

// CtxRequestID get request id from context.Context
// CtxRequestID 从 context.Context 取请求 id。
func CtxRequestID(ctx context.Context) string {
	v := ctx.Value(ContextRequestIDKey)
	if str, ok := v.(string); ok {
		return str
	}
	return ""
}

// HeaderRequestID get request id from the header
// HeaderRequestID 从请求头取请求 id。
func HeaderRequestID(c *gin.Context) string {
	return c.Request.Header.Get(HeaderXRequestIDKey)
}
