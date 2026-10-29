// Package bodylimit 限制请求体大小。
//
// gin 默认不限制请求体：`ShouldBind` 会把整个 body 读进内存，一个未认证的客户端就能用超大的
// 请求体耗尽内存。把本中间件挂在最前面，超出上限的读取会失败。
package bodylimit

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// DefaultMaxBytes 是默认上限（4MiB）。
const DefaultMaxBytes int64 = 4 << 20

type Builder struct {
	maxBytes int64
}

func NewBuilder() *Builder {
	return &Builder{maxBytes: DefaultMaxBytes}
}

// SetMaxBytes 设置请求体上限。n<=0 时忽略。
func (b *Builder) SetMaxBytes(n int64) *Builder {
	if n > 0 {
		b.maxBytes = n
	}
	return b
}

// Build 返回中间件。
//
// 声明了 Content-Length 且超限的请求直接返回 413，不读 body；没声明长度（分块传输）或声明有误的，
// 由 http.MaxBytesReader 在读取超限时让读取返回 *http.MaxBytesError，绑定函数会因此报错，
// 同时它会通知服务器关闭连接。
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
