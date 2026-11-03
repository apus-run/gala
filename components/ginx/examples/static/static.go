// Package static demonstrates serving local files under ./static by path,
// aborting the chain when the file exists so business routes never see those
// paths. Shortcut kept honest: the builder's root and URL prefix have no
// setters yet, so the example serves the package directory's static/ folder.
// Package static 演示按路径提供 ./static 下的本地文件：文件存在时中断
// 链路，业务路由因此不会看到这些路径。如实说明 shortcut：builder 的根
// 目录和 URL 前缀还没有 setter，所以本示例提供的是包目录下的 static/
// 文件夹。
package static

import (
	middleware "github.com/apus-run/gala/components/ginx/middlewares/static"
	"github.com/gin-gonic/gin"
)

// New builds the demo engine with the static file middleware | New 构建挂了静态文件中间件的示例引擎.
func New() *gin.Engine {
	r := gin.New()
	r.Use(middleware.NewBuilder().Build()) // root ./static, URL prefix / | 根目录 ./static，URL 前缀 /
	r.GET("/api/health", func(c *gin.Context) { c.String(200, "ok") })
	return r
}
