// Package circuitbreaker demonstrates protecting one route group behind a
// breaker built with the chaining style: repeated upstream failures open the
// circuit and further requests fail fast, while liveness outside the group
// stays reachable. The builder's breaker handle carries the operations API.
package circuitbreaker

import (
	"net/http"
	"time"

	middleware "github.com/apus-run/gala/components/ginx/middlewares/circuitbreaker"
	"github.com/gin-gonic/gin"
)

type Server struct {
	Engine  *gin.Engine
	Breaker *middleware.Builder
}

func New() *Server {
	breaker := middleware.NewBuilder().
		SetFailureThreshold(2).
		SetTimeout(30 * time.Second)
	r := gin.New()
	api := r.Group("/api")
	api.Use(breaker.Build())
	api.GET("/orders/:id", func(c *gin.Context) {
		if c.Param("id") == "0" { // the dependency behind this group is down
			c.JSON(http.StatusBadGateway, gin.H{"error": "upstream unavailable"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"id": c.Param("id")})
	})
	r.GET("/healthz", func(c *gin.Context) { c.Status(http.StatusOK) })
	return &Server{Engine: r, Breaker: breaker}
}
