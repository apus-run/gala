// Package templatex parses, caches and renders html/template templates through
// a concurrency-safe registry | templatex 通过并发安全的注册表解析、缓存并渲染
// html/template 模板.
//
// # Notes | 说明
//
// The registry is an LRU cache (65536 entries): a files set, an inline string
// or an fs.FS plus glob patterns is parsed once and reused; cached entries —
// including ones holding a parse error — live until Reset, so changed sources
// are picked up only after Reset. It builds on html/template only, so output
// is HTML-escaped; the built-in "raw" function returns unescaped HTML.
// Load errors are stored on the Renderer and surface at Render time, not from
// the Load* calls. AddFuncs is for initialization and is not safe to call
// concurrently with Load* or after the first parse.
// 注册表是一个 LRU 缓存（65536 项）：一组文件、一段内联字符串或一个 fs.FS 加 glob
// 模式只解析一次并复用；缓存项（包括持有解析错误的）存活到 Reset 为止，因此源文件
// 变更只有在 Reset 后才会生效。仅基于 html/template，输出会做 HTML 转义；内置
// "raw" 函数返回未转义 HTML。加载错误记录在 Renderer 上，在 Render 时才暴露，而非
// Load* 调用时。AddFuncs 仅用于初始化，与 Load* 并发调用或首次解析之后调用都不安全。
//
// # Example | 示例
//
//	r := templatex.NewRegistry()
//	html, err := r.LoadFiles("layout.html", "content.html").
//		Render(map[string]any{"name": "John"})
//	// a second LoadFiles with the same set reuses the cached parse
//	again, err := r.LoadFiles("layout.html", "content.html").
//		Render(map[string]any{"name": "Jane"})
package templatex

import (
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"maps"
	"path/filepath"

	"github.com/hashicorp/golang-lru/v2"
)

// NewRegistry creates and initializes a new templates registry with some
// defaults (eg. global "raw" template function for unescaped HTML). Use the
// Registry.Load* methods to load templates into the registry.
//
// NewRegistry 创建并初始化一个新的模板注册表，自带一些默认值（如输出未转义
// HTML 的全局 "raw" 模板函数）。使用 Registry.Load* 方法向注册表加载模板。
func NewRegistry() *Registry {
	l, err := lru.New[string, *Renderer](65536)
	if err != nil {
		// Unreachable for a fixed positive size; panic rather than return
		// a nil registry.
		// 固定正数容量下不可达；与其返回 nil 注册表不如直接 panic。
		panic(err)
	}
	return &Registry{
		cache: l,
		funcs: template.FuncMap{
			"raw": func(str string) template.HTML {
				return template.HTML(str)
			},
		},
	}
}

// Registry defines a templates registry that is safe to be used by multiple
// goroutines. Use the Registry.Load* methods to load templates into the registry.
//
// Registry 定义一个可被多个 goroutine 安全并发使用的模板注册表。
// 使用 Registry.Load* 方法向注册表加载模板。
type Registry struct {
	cache *lru.Cache[string, *Renderer]
	funcs template.FuncMap
}

// AddFuncs registers new global template functions.
//
// The key of each map entry is the function name that will be used in the templates.
// If a function with the map entry name already exists it will be replaced with the new one.
//
// The value of each map entry is a function that must have either a
// single return value, or two return values of which the second has type error.
//
// Example:
//
//	r.AddFuncs(map[string]any{
//	  "toUpper": func(str string) string {
//	      return strings.ToUppser(str)
//	  },
//	  ...
//	})
//
// AddFuncs is meant for initialization: it is not safe to call it concurrently
// with the Load* methods or after the first template has been parsed.
//
// AddFuncs 注册新的全局模板函数。
//
// 每个 map 项的 key 是模板中要使用的函数名；同名函数已存在时会被新函数替换。
//
// 每个 map 项的 value 是一个函数：要么只有一个返回值，要么有两个返回值且
// 第二个类型为 error（示例见上方代码）。
//
// AddFuncs 仅用于初始化：与 Load* 方法并发调用，或在首个模板解析之后调用，
// 都不安全。
func (r *Registry) AddFuncs(funcs map[string]any) *Registry {
	maps.Copy(r.funcs, funcs)

	return r
}

// Cache keys carry a source prefix: files, strings and FS share one cache,
// and without a prefix a template text that happens to equal a file path
// would hit the file template. %#v keeps the boundary between filenames.
//
// 缓存 key 带来源前缀：文件、字符串、FS 三种来源共用一个缓存，不加前缀的话，
// 模板文本恰好等于某个文件路径时会取到文件模板。%#v 保留了文件名之间的边界。
func filesKey(filenames []string) string { return fmt.Sprintf("files:%#v", filenames) }
func stringKey(text string) string       { return "string:" + text }
func fsKey(fsys fs.FS, patterns []string) string {
	return fmt.Sprintf("fs:%v%#v", fsys, patterns)
}

// LoadFiles caches (if not already) the specified filenames set as a
// single template and returns a ready to use Renderer instance. There must
// be at least 1 filename specified.
//
// LoadFiles 将指定的文件名集合（若尚未缓存）缓存为单个模板，并返回可直接
// 使用的 Renderer 实例。至少要指定 1 个文件名。
func (r *Registry) LoadFiles(filenames ...string) *Renderer {
	if len(filenames) == 0 {
		return &Renderer{parseError: errors.New("at least 1 filename is required")}
	}

	// The prefix separates the key spaces of the three sources; %#v keeps
	// the boundary between filenames, so "a,b" (one file) and "a","b"
	// (two files) do not share a key.
	// 前缀把文件、字符串、FS 三种来源的 key 空间分开；%#v 保留了文件名之间的边界，
	// "a,b"（一个文件）和 "a","b"（两个文件）不会得到同一个 key
	key := filesKey(filenames)

	v, ok := r.cache.Get(key)
	if ok {
		return v
	}

	// parse and cache
	// 解析并缓存
	tpl, err := template.New(filepath.Base(filenames[0])).Funcs(r.funcs).ParseFiles(filenames...)
	v = &Renderer{template: tpl, parseError: err}
	r.cache.Add(key, v)

	return v
}

// LoadString caches (if not already) the specified inline string as a
// single template and returns a ready to use Renderer instance.
//
// LoadString 将指定的内联字符串（若尚未缓存）缓存为单个模板，并返回可直接
// 使用的 Renderer 实例。
func (r *Registry) LoadString(text string) *Renderer {
	// The template text cannot serve as the key directly: a text that
	// happens to equal a filename would hit the file template cached
	// by LoadFiles.
	// 不能直接拿模板文本当 key：文本恰好等于某个文件名时，会命中 LoadFiles 缓存的文件模板
	key := stringKey(text)
	v, ok := r.cache.Get(key)
	if ok {
		return v
	}

	// parse and cache
	// 解析并缓存
	tpl, err := template.New("").Funcs(r.funcs).Parse(text)
	v = &Renderer{template: tpl, parseError: err}
	r.cache.Add(key, v)

	return v
}

// LoadFS caches (if not already) the specified fs and globPatterns
// pair as single template and returns a ready to use Renderer instance.
//
// There must be at least 1 file matching the provided globPattern(s)
// (note that most file names serves as glob patterns matching themselves).
//
// LoadFS 将指定的 fs 与 globPatterns 组合（若尚未缓存）缓存为单个模板，并返回
// 可直接使用的 Renderer 实例。至少要有 1 个文件匹配给定的 glob 模式（注意大多
// 数文件名本身就是匹配自身的 glob 模式）。
func (r *Registry) LoadFS(fsys fs.FS, globPatterns ...string) *Renderer {
	// %#v keeps distinct pattern sets distinct: %v renders ["x y"] and
	// ["x", "y"] identically.
	// %#v 让不同的模式集合保持不同：%v 会把 ["x y"] 和 ["x", "y"] 渲染成一样
	key := fsKey(fsys, globPatterns)

	v, ok := r.cache.Get(key)
	if ok {
		return v
	}

	// find the first file to use as template name (it is required when specifying Funcs)
	// 找第一个文件用作模板名（指定 Funcs 时它是必需的）
	var firstFilename string
	if len(globPatterns) > 0 {
		list, _ := fs.Glob(fsys, globPatterns[0])
		if len(list) > 0 {
			firstFilename = filepath.Base(list[0])
		}
	}

	tpl, err := template.New(firstFilename).Funcs(r.funcs).ParseFS(fsys, globPatterns...)
	v = &Renderer{template: tpl, parseError: err}
	r.cache.Add(key, v)

	return v
}

// Reset removes all cached templates. Cached entries — including ones holding
// a parse error — live for the lifetime of the process, so this is the way to
// make the registry pick up changed template sources (e.g. during development).
//
// Reset 移除所有已缓存的模板。缓存项（包括持有解析错误的）存活于进程整个生命
// 周期，因此要让注册表感知模板源的变更（例如开发期间）只能走这里。
func (r *Registry) Reset() {
	r.cache.Purge()
}
