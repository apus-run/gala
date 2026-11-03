// Package bodylimit limits the request body size | bodylimit 限制请求体大小.
//
// gin does not limit the request body by default: ShouldBind reads the whole
// body into memory, so a single unauthenticated client can exhaust memory with
// an oversized body. Mount this middleware first; reads beyond the limit fail.
// gin 默认不限制请求体：ShouldBind 会把整个 body 读进内存，一个未认证的客户端
// 就能用超大的请求体耗尽内存。把本中间件挂在最前面，超出上限的读取会失败。
package bodylimit

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// DefaultMaxBytes is the default limit (4MiB) | 默认上限（4MiB）。
const DefaultMaxBytes int64 = 4 << 20

// Builder assembles the body-limit middleware | Builder 组装请求体限制中间件.
type Builder struct {
	maxBytes int64
}

// NewBuilder creates a Builder with the default limit | NewBuilder 按默认上限创建 Builder。
func NewBuilder() *Builder {
	return &Builder{maxBytes: DefaultMaxBytes}
}

// SetMaxBytes sets the body size limit; n<=0 is ignored
// | SetMaxBytes 设置请求体上限，n<=0 时忽略。
func (b *Builder) SetMaxBytes(n int64) *Builder {
	if n > 0 {
		b.maxBytes = n
	}
	return b
}

// Build returns the middleware | Build 返回中间件。
//
// Requests declaring an over-limit Content-Length get 413 immediately without
// reading the body. For chunked or mis-declared bodies, http.MaxBytesReader
// makes reads past the limit return *http.MaxBytesError, which binding
// functions surface; it also asks the server to close the connection.
// 声明了 Content-Length 且超限的请求直接返回 413，不读 body；没声明长度
// （分块传输）或声明有误的，由 http.MaxBytesReader 在读取超限时让读取返回
// *http.MaxBytesError，绑定函数会因此报错，同时它会通知服务器关闭连接。
func (b *Builder) Build() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.ContentLength > b.maxBytes {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{
				"code":    http.StatusRequestEntityTooLarge,
				"message": http.StatusText(http.StatusRequestEntityTooLarge),
			})
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, b.maxBytes)
		c.Next()
	}
}
