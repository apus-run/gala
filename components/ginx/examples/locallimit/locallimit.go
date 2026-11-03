// Package locallimit demonstrates the in-process active-request cap: at most
// N handlers run concurrently per process; the excess gets 429 before any
// downstream work starts. For a cluster-wide cap see redislimit.
// Package locallimit 演示进程内的并发请求上限：每个进程最多同时运行 N 个
// handler，超出的请求在任何下游工作开始前收到 429。集群级上限见 redislimit。
package locallimit

import (
	"time"

	"github.com/apus-run/gala/components/ginx/middlewares/activelimit/locallimit"
	"github.com/gin-gonic/gin"
)

// New builds the demo engine with a local active-request limit | New 构建挂了进程内并发上限中间件的示例引擎.
func New() *gin.Engine {
	r := gin.New()
	r.Use(locallimit.NewLocalActiveLimit(2).Build())
	r.POST("/import", func(c *gin.Context) { // heavy, bounded work | 重度且有界的处理
		time.Sleep(50 * time.Millisecond)
		c.String(200, "imported")
	})
	return r
}
