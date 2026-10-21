// Package error demonstrates translating handler errors into HTTP responses:
// handlers attach typed errorsx errors to the context and stay clean of
// response writing; the middleware renders the last one with its code.
package error

import (
	"net/http"

	middleware "github.com/apus-run/gala/components/ginx/middlewares/error"
	"github.com/apus-run/gala/pkg/errorsx"
	"github.com/gin-gonic/gin"
)

func New() *gin.Engine {
	r := gin.New()
	r.Use(middleware.NewBuilder().Build())
	r.GET("/orders/:id", func(c *gin.Context) {
		if c.Param("id") != "42" {
			_ = c.Error(errorsx.InvalidParams("ORDER_NOT_FOUND").
				WithMessage("order does not exist").
				KV("order_id", c.Param("id")))
			return
		}
		c.JSON(http.StatusOK, gin.H{"id": "42"})
	})
	return r
}
