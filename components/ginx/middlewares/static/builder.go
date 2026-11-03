// Package static serves local files from a gin router, aborting the chain
// when a file matches | static 在 gin 路由上提供本地文件，命中文件即中止链.
//
// # Notes | 说明
//
// Builder defaults to serving ./static at "/"; only existing files (or
// directories honoring the index rule) are handled, everything else falls
// through to later routes. urlPrefix and root have no setters — the
// defaults are the only configuration. [LocalFile] builds the underlying
// [ServeFileSystem]; indexes=false forbids directory listings.
// Builder 默认在 "/" 提供 ./static；只有存在的文件（或遵守 index 规则的目录）
// 会被处理，其余请求继续走后续路由。urlPrefix 与 root 没有 setter——默认值
// 即全部配置。[LocalFile] 构建底层 [ServeFileSystem]；indexes=false 禁止目录列表。
//
// # Example | 示例
//
//	r.Use(static.NewBuilder().Build())
package static

import (
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/gin-gonic/gin"
)

// Builder assembles the static-file middleware | Builder 组装静态文件中间件.
type Builder struct {
	urlPrefix string
	root      string
}

// NewBuilder creates a Builder serving ./static at "/" | NewBuilder 创建在 "/" 提供 ./static 的 Builder。
func NewBuilder() *Builder {
	return &Builder{
		urlPrefix: "/",
		root:      "./static",
	}
}

// Build returns the middleware serving files under root; requests hitting a
// file abort the chain | Build 返回提供 root 下文件的中间件；命中文件的请求会中止链。
func (b *Builder) Build() gin.HandlerFunc {
	fs := LocalFile(b.root, false)
	fileServer := http.FileServer(fs)
	if b.urlPrefix != "" {
		fileServer = http.StripPrefix(b.urlPrefix, fileServer)
	}
	return func(c *gin.Context) {
		if fs.Exists(b.urlPrefix, c.Request.URL.Path) {
			fileServer.ServeHTTP(c.Writer, c.Request)
			c.Abort()
		}
	}
}

// INDEX is the directory index file name | INDEX 是目录索引文件名.
const INDEX = "index.html"

// ServeFileSystem is a FileSystem that can test existence
// | ServeFileSystem 是能判断文件是否存在的 FileSystem.
type ServeFileSystem interface {
	http.FileSystem
	Exists(prefix string, path string) bool
}

type localFileSystem struct {
	http.FileSystem
	root    string
	indexes bool
}

// LocalFile creates a filesystem rooted at root | LocalFile 创建以 root 为根的文件系统。
func LocalFile(root string, indexes bool) *localFileSystem {
	return &localFileSystem{
		FileSystem: gin.Dir(root, indexes),
		root:       root,
		indexes:    indexes,
	}
}

// Exists reports whether a file exists under prefix, honoring the index rule
// for directories | Exists 判断 prefix 下文件是否存在；目录需遵守 index 规则。
func (l *localFileSystem) Exists(prefix string, filepath string) bool {
	if p := strings.TrimPrefix(filepath, prefix); len(p) < len(filepath) {
		name := path.Join(l.root, p)
		stats, err := os.Stat(name)
		if err != nil {
			return false
		}
		if stats.IsDir() {
			if !l.indexes {
				index := path.Join(name, INDEX)
				_, err := os.Stat(index)
				if err != nil {
					return false
				}
			}
		}
		return true
	}
	return false
}

//  static.NewBuilder().Build("/", "./dist") | 用法示例
