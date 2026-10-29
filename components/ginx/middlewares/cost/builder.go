package cost

import (
	"log"
	"time"

	"github.com/gin-gonic/gin"
)

type Builder struct{}

func NewBuilder() *Builder {
	return &Builder{}
}

func (b *Builder) Build() gin.HandlerFunc {
	// 使用函数回调
	return func(c *gin.Context) {
		// 记录开始时间
		start := time.Now()

		// 只记路径：RequestURI 带着查询串，里面的 token、password 会进日志
		path := c.Request.URL.Path
		log.Printf("api uri start: %v", path)
		// 使用next执行具体的业务逻辑
		c.Next()

		// 记录结束时间
		end := time.Now()
		cost := end.Sub(start)
		log.Printf("api uri end: %v, cost: %v", path, cost.Seconds())
	}
}

// cost.NewBuilder().Build()
