// Package static demonstrates serving local files under ./static by path,
// aborting the chain when the file exists so business routes never see those
// paths. Shortcut kept honest: the builder's root and URL prefix have no
// setters yet, so the example serves the package directory's static/ folder.
package static

import (
	middleware "github.com/apus-run/gala/components/ginx/middlewares/static"
	"github.com/gin-gonic/gin"
)

func New() *gin.Engine {
	r := gin.New()
	r.Use(middleware.NewBuilder().Build()) // root ./static, URL prefix /
	r.GET("/api/health", func(c *gin.Context) { c.String(200, "ok") })
	return r
}
