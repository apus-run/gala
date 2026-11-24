package ginx

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

// SetupEmbedAssets registers GET/HEAD routes serving embedded assets from fs
// under the given relative paths | SetupEmbedAssets 为给定相对路径注册
// GET/HEAD 路由，从嵌入文件系统 fs 提供静态资源。
func SetupEmbedAssets(rg *gin.RouterGroup, fs http.FileSystem, relativePaths ...string) {
	handler := func(c *gin.Context) {
		c.FileFromFS(strings.TrimPrefix(c.Request.URL.Path, rg.BasePath()), fs)
	}

	for _, relativePath := range relativePaths {
		urlPattern := relativePath
		if urlPattern != "/" {
			urlPattern = path.Join(relativePath, "/*filepath")
		}
		rg.GET(urlPattern, handler)
		rg.HEAD(urlPattern, handler)
	}
}

// SetupStaticAssets walks dir and registers its top-level subdirectories as
// static routes, plus index.html as the root file. A walk error is returned
// instead of exiting the process, so the caller decides how to react.
// SetupStaticAssets 遍历 dir，把顶层子目录注册为静态路由，并把 index.html
// 注册为根文件。遍历错误作为返回值交给调用方决定如何处理，不再直接退出进程。
func SetupStaticAssets(rg *gin.RouterGroup, dir string) error {
	_, rootDirName := filepath.Split(dir)
	staticLoader := func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() && info.Name() != rootDirName {
			rg.Static(info.Name(), path)
			return nil
		}

		if info.Name() == "index.html" {
			rg.StaticFile("/", path)
		}

		return nil
	}

	return filepath.Walk(dir, staticLoader)
}
