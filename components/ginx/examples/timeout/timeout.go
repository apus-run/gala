// Package timeout demonstrates bounding handler time: the request context
// gets a deadline and handlers must respect it. shortcut: the middleware
// panics on deadline, so an outer recovery answers 500, not 504; upgrade the
// middleware when the status distinction matters.
// Package timeout 演示限定 handler 的执行时间：请求上下文带有截止时间，
// handler 必须遵守。shortcut: 中间件在到达截止时间时 panic，因此外层
// recovery 回 500 而非 504；若需要区分状态码，请升级该中间件。
package timeout

import (
	"time"

	mwrecovery "github.com/apus-run/gala/components/ginx/middlewares/recovery"
	middleware "github.com/apus-run/gala/components/ginx/middlewares/timeout"
	"github.com/gin-gonic/gin"
)

// New builds the demo engine with the timeout middleware | New 构建挂了超时中间件的示例引擎.
func New() *gin.Engine {
	r := gin.New()
	r.Use(mwrecovery.NewBuilder().Build()) // catches the deadline panic below | 捕获下方中间件在到达截止时间时触发的 panic
	r.Use(middleware.NewBuilder().SetTimeout(100 * time.Millisecond).Build())
	r.GET("/report", func(c *gin.Context) {
		select { // a real handler polls the request context while working | 真实 handler 在工作时会轮询请求上下文
		case <-time.After(time.Second):
			c.String(200, "slow report done")
		case <-c.Request.Context().Done():
			return
		}
	})
	return r
}
