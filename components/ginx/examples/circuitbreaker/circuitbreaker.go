// Package circuitbreaker demonstrates protecting one route group behind a
// breaker built with the chaining style: repeated upstream failures open the
// circuit and further requests fail fast, while liveness outside the group
// stays reachable. The builder's breaker handle carries the operations API.
// Package circuitbreaker 演示用链式风格构建的熔断器保护一个路由组：上游
// 连续失败会打开熔断器，后续请求快速失败，而组外的存活检查仍然可达。
// Builder 的 breaker 句柄承载运维 API。
package circuitbreaker

import (
	"net/http"
	"time"

	middleware "github.com/apus-run/gala/components/ginx/middlewares/circuitbreaker"
	"github.com/gin-gonic/gin"
)

// Server bundles the demo engine and its breaker builder | Server 打包示例引擎与其熔断 builder.
type Server struct {
	// Engine is the demo HTTP engine | 示例 HTTP 引擎.
	Engine *gin.Engine
	// Breaker exposes the breaker's operations API | Breaker 暴露熔断器的运维 API.
	Breaker *middleware.Builder
}

// New builds the demo engine protected by a circuit breaker | New 构建受熔断器保护的示例引擎.
func New() *Server {
	breaker := middleware.NewBuilder().
		SetFailureThreshold(2).
		SetTimeout(30 * time.Second)
	r := gin.New()
	api := r.Group("/api")
	api.Use(breaker.Build())
	api.GET("/orders/:id", func(c *gin.Context) {
		if c.Param("id") == "0" { // the dependency behind this group is down | 该组背后的依赖已宕机
			c.JSON(http.StatusBadGateway, gin.H{"error": "upstream unavailable"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"id": c.Param("id")})
	})
	r.GET("/healthz", func(c *gin.Context) { c.Status(http.StatusOK) })
	return &Server{Engine: r, Breaker: breaker}
}
