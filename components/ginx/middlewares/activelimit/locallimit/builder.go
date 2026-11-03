// Package locallimit caps the number of concurrent in-process requests |
// locallimit 限制进程内并发请求的数量.
//
// # Notes | 说明
//
// The count is per instance and maintained with atomics; excess requests
// are rejected with a bare 429, nothing else. SetMaxActive can adjust the
// cap at runtime. For cross-process or per-key rate control see the sibling
// ratelimit package.
// 计数按实例维护，用原子操作完成；超限请求直接以 429 拒绝，无其他动作。
// SetMaxActive 可运行时调整上限。跨进程或按键限流见同目录的 ratelimit 包。
//
// # Example | 示例
//
//	r.Use(locallimit.NewLocalActiveLimit(100).Build())
package locallimit

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/atomic"
)

// LocalActiveLimit caps concurrent in-process requests | LocalActiveLimit 进程内并发限流.
type LocalActiveLimit struct {
	// Max allowed active count | 最大限制个数.
	maxActive *atomic.Int64
	// Current active count | 当前活跃个数.
	countActive *atomic.Int64
}

// NewLocalActiveLimit creates a process-wide active-request limit
// | NewLocalActiveLimit 创建全局限流（进程内并发数上限）。
func NewLocalActiveLimit(maxActive int64) *LocalActiveLimit {
	return &LocalActiveLimit{
		maxActive:   atomic.NewInt64(maxActive),
		countActive: atomic.NewInt64(0),
	}
}

// SetMaxActive sets the max concurrent requests | SetMaxActive 设置最大并发请求数。
func (a *LocalActiveLimit) SetMaxActive(maxActive int64) *LocalActiveLimit {
	a.maxActive.Store(maxActive)
	return a
}

// Build returns the gin middleware, rejecting excess requests with 429
// | Build 返回 gin 中间件，超限请求以 429 拒绝。
func (a *LocalActiveLimit) Build() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		current := a.countActive.Add(1)
		defer func() {
			a.countActive.Sub(1)
		}()
		if current <= a.maxActive.Load() {
			ctx.Next()
		} else {
			ctx.AbortWithStatus(http.StatusTooManyRequests)
		}
	}
}
