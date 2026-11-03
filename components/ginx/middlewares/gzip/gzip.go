// Package gzip compresses gin responses and optionally decompresses gzip
// request bodies | gzip 压缩 gin 响应，并可选解压 gzip 请求体.
//
// # Notes | 说明
//
// Compression happens only when the client sends Accept-Encoding: gzip;
// SSE responses, upgraded connections and excluded extensions, paths and
// regexs are skipped (.png/.gif/.jpeg/.jpg by default). gzip.Writer
// instances come from a sync.Pool. Request decompression is opt-in via
// WithGzipDecompressFn plus [DefaultDecompressHandle].
// 仅当客户端携带 Accept-Encoding: gzip 时压缩；SSE 响应、连接升级以及被排除
// 的扩展名、路径、正则直接跳过（默认排除 .png/.gif/.jpeg/.jpg）。gzip.Writer
// 经 sync.Pool 复用。请求体解压需显式开启：WithGzipDecompressFn 配合
// [DefaultDecompressHandle]。
//
// # Example | 示例
//
//	r.Use(gzip.NewBuilder().Build())
package gzip

import (
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

const (
	// BestCompression is the gzip level for the smallest output | gzip 压缩比最高档位。
	BestCompression = gzip.BestCompression
	// BestSpeed is the gzip level for the fastest compression | gzip 压缩速度最快档位。
	BestSpeed = gzip.BestSpeed
	// DefaultCompression is the gzip default level | gzip 默认压缩档位。
	DefaultCompression = gzip.DefaultCompression
	// NoCompression disables compression | 不压缩。
	NoCompression = gzip.NoCompression
)

// Builder assembles the gzip middleware | Builder 组装 gzip 中间件.
type Builder struct {
	level int
}

// NewBuilder creates a Builder at the default compression level
// | NewBuilder 按默认压缩级别创建 Builder。
func NewBuilder() *Builder {
	return &Builder{
		level: DefaultCompression,
	}
}

// Build returns the gzip middleware | Build 返回 gzip 中间件。
func (b *Builder) Build(options ...GzipOption) gin.HandlerFunc {
	return newGzipHandler(b.level, options...).Handle
}

// SetLevel sets the gzip compression level | SetLevel 设置 gzip 压缩级别。
func (b *Builder) SetLevel(level int) *Builder {
	b.level = level
	return b
}

type gzipWriter struct {
	gin.ResponseWriter
	writer *gzip.Writer
}

// WriteString writes through the gzip writer | WriteString 经 gzip writer 写出.
func (g *gzipWriter) WriteString(s string) (int, error) {
	g.Header().Del("Content-Length")
	return g.writer.Write([]byte(s))
}

// Write writes through the gzip writer | Write 经 gzip writer 写出.
func (g *gzipWriter) Write(data []byte) (int, error) {
	g.Header().Del("Content-Length")
	return g.writer.Write(data)
}

// Fix: https://github.com/mholt/caddy/issues/38 | 修复 caddy #38 的 Content-Length 问题
func (g *gzipWriter) WriteHeader(code int) {
	g.Header().Del("Content-Length")
	g.ResponseWriter.WriteHeader(code)
}

type gzipHandler struct {
	*GzipOptions
	gzPool sync.Pool
}

func newGzipHandler(level int, options ...GzipOption) *gzipHandler {
	// Copy first: setters mutate in place, so using the global DefaultOptions
	// directly would let multiple Build calls overwrite each other.
	// 复制一份：setter 会就地修改，直接用全局 DefaultOptions 会让多个 Build 互相覆盖
	opts := *DefaultOptions
	handler := &gzipHandler{
		GzipOptions: &opts,
		gzPool: sync.Pool{
			New: func() any {
				gz, err := gzip.NewWriterLevel(io.Discard, level)
				if err != nil {
					panic(err)
				}
				return gz
			},
		},
	}
	for _, setter := range options {
		setter.apply(handler.GzipOptions)
	}
	return handler
}

// Handle compresses the response when the client accepts gzip | Handle 在客户端接受 gzip 时压缩响应.
func (g *gzipHandler) Handle(c *gin.Context) {
	if fn := g.DecompressFn; fn != nil && c.Request.Header.Get("Content-Encoding") == "gzip" {
		fn(c)
	}

	if !g.shouldCompress(c.Request) {
		return
	}

	gz := g.gzPool.Get().(*gzip.Writer)
	defer g.gzPool.Put(gz)
	defer gz.Reset(io.Discard)
	gz.Reset(c.Writer)

	c.Header("Content-Encoding", "gzip")
	c.Header("Vary", "Accept-Encoding")
	c.Writer = &gzipWriter{c.Writer, gz}
	defer func() {
		gz.Close()
		c.Header("Content-Length", fmt.Sprint(c.Writer.Size()))
	}()
	c.Next()
}

func (g *gzipHandler) shouldCompress(req *http.Request) bool {
	if !strings.Contains(req.Header.Get("Accept-Encoding"), "gzip") ||
		strings.Contains(req.Header.Get("Connection"), "Upgrade") ||
		strings.Contains(req.Header.Get("Accept"), "text/event-stream") {
		return false
	}

	extension := filepath.Ext(req.URL.Path)
	if g.ExcludedExtensions.Contains(extension) {
		return false
	}

	if g.ExcludedPaths.Contains(req.URL.Path) {
		return false
	}
	if g.ExcludedPathesRegexs.Contains(req.URL.Path) {
		return false
	}

	return true
}

// DefaultExcludedExtentions are the extensions never compressed | 默认不压缩的扩展名.
var DefaultExcludedExtentions = NewExcludedExtensions([]string{
	".png", ".gif", ".jpeg", ".jpg",
})

// DefaultOptions is the default gzip option set | 默认 gzip 选项.
var DefaultOptions = &GzipOptions{
	ExcludedExtensions: DefaultExcludedExtentions,
}

// GzipOptions holds the gzip middleware options | GzipOptions 持有 gzip 中间件选项.
type GzipOptions struct {
	// ExcludedExtensions are extensions never compressed | 不压缩的扩展名。
	ExcludedExtensions ExcludedExtensions
	// ExcludedPaths are request paths excluded by prefix | 按前缀排除的请求路径。
	ExcludedPaths ExcludedPaths
	// ExcludedPathesRegexs are request paths excluded by regexp | 按正则排除的请求路径。
	ExcludedPathesRegexs ExcludedPathesRegexs
	// DecompressFn handles gzip-encoded request bodies | 处理 gzip 编码请求体的解压函数。
	DecompressFn func(c *gin.Context)
}

// GzipOption configures GzipOptions | GzipOption 配置 GzipOptions。
type GzipOption interface {
	apply(*GzipOptions)
}

// GzipOptionFunc adapts a plain function to GzipOption.
// GzipOptionFunc 把普通函数适配为 GzipOption。
type GzipOptionFunc func(*GzipOptions)

func (f GzipOptionFunc) apply(o *GzipOptions) {
	f(o)
}

// WithGzipExcludedExtensions sets extensions to skip | WithGzipExcludedExtensions 设置跳过压缩的扩展名。
func WithGzipExcludedExtensions(args []string) GzipOption {
	return GzipOptionFunc(func(o *GzipOptions) {
		o.ExcludedExtensions = NewExcludedExtensions(args)
	})
}

// WithGzipExcludedPaths sets paths to skip | WithGzipExcludedPaths 设置跳过压缩的路径。
func WithGzipExcludedPaths(args []string) GzipOption {
	return GzipOptionFunc(func(o *GzipOptions) {
		o.ExcludedPaths = NewExcludedPaths(args)
	})
}

// WithGzipExcludedPathsRegexs sets path regexps to skip | WithGzipExcludedPathsRegexs 设置跳过压缩的路径正则。
func WithGzipExcludedPathsRegexs(args []string) GzipOption {
	return GzipOptionFunc(func(o *GzipOptions) {
		o.ExcludedPathesRegexs = NewExcludedPathesRegexs(args)
	})
}

// WithGzipDecompressFn sets the request decompression handler
// | WithGzipDecompressFn 设置请求解压处理函数。
func WithGzipDecompressFn(decompressFn func(c *gin.Context)) GzipOption {
	return GzipOptionFunc(func(o *GzipOptions) {
		o.DecompressFn = decompressFn
	})
}

// ExcludedExtensions uses a map for better lookup performance
// | ExcludedExtensions 用 map（safemap）提升查找性能。
type ExcludedExtensions map[string]bool

// NewExcludedExtensions builds a set from extensions | NewExcludedExtensions 由扩展名构建集合。
func NewExcludedExtensions(extensions []string) ExcludedExtensions {
	res := make(ExcludedExtensions)
	for _, e := range extensions {
		res[e] = true
	}
	return res
}

// Contains reports whether target is excluded | Contains 判断 target 是否被排除。
func (e ExcludedExtensions) Contains(target string) bool {
	_, ok := e[target]
	return ok
}

// ExcludedPaths are request paths excluded by prefix | ExcludedPaths 按前缀排除的请求路径.
type ExcludedPaths []string

// NewExcludedPaths builds ExcludedPaths | NewExcludedPaths 构建 ExcludedPaths。
func NewExcludedPaths(paths []string) ExcludedPaths {
	return ExcludedPaths(paths)
}

// Contains reports whether the request URI has an excluded prefix
// | Contains 判断请求 URI 是否命中被排除前缀。
func (e ExcludedPaths) Contains(requestURI string) bool {
	for _, path := range e {
		if strings.HasPrefix(requestURI, path) {
			return true
		}
	}
	return false
}

// ExcludedPathesRegexs are request paths excluded by regexp | ExcludedPathesRegexs 按正则排除的请求路径.
type ExcludedPathesRegexs []*regexp.Regexp

// NewExcludedPathesRegexs compiles regexs, panicking on invalid patterns
// | NewExcludedPathesRegexs 编译正则，非法模式会 panic。
func NewExcludedPathesRegexs(regexs []string) ExcludedPathesRegexs {
	result := make([]*regexp.Regexp, len(regexs))
	for i, reg := range regexs {
		result[i] = regexp.MustCompile(reg)
	}
	return result
}

// Contains reports whether the request URI matches an excluded regexp
// | Contains 判断请求 URI 是否命中被排除正则。
func (e ExcludedPathesRegexs) Contains(requestURI string) bool {
	for _, reg := range e {
		if reg.MatchString(requestURI) {
			return true
		}
	}
	return false
}

// DefaultDecompressHandle gunzips the request body and clears the encoding
// headers, aborting with 400 on bad input | DefaultDecompressHandle 解压请求体
// 并清除编码相关头，输入非法时以 400 中止。
func DefaultDecompressHandle(c *gin.Context) {
	if c.Request.Body == nil {
		return
	}
	r, err := gzip.NewReader(c.Request.Body)
	if err != nil {
		_ = c.AbortWithError(http.StatusBadRequest, err)
		return
	}
	c.Request.Header.Del("Content-Encoding")
	c.Request.Header.Del("Content-Length")
	c.Request.Body = r
}
