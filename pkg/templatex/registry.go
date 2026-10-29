// Package templatex is a thin wrapper around the standard html/template
// and text/template packages that implements a convenient registry to
// load and cache templates on the fly concurrently.
//
// It was created to assist the JSVM plugin HTML rendering, but could be used in other Go code.
//
// Example:
//
//	registry := templatex.NewRegistry()
//
//	html1, err := registry.LoadFiles(
//		// the files set wil be parsed only once and then cached
//		"layout.html",
//		"content.html",
//	).Render(map[string]any{"name": "John"})
//
//	html2, err := registry.LoadFiles(
//		// reuse the already parsed and cached files set
//		"layout.html",
//		"content.html",
//	).Render(map[string]any{"name": "Jane"})
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

// NewRegistry creates and initializes a new templates registry with
// some defaults (eg. global "raw" template function for unescaped HTML).
//
// Use the Registry.Load* methods to load templates into the registry.
func NewRegistry() *Registry {
	l, err := lru.New[string, *Renderer](65536)
	if err != nil { // unreachable for a fixed positive size; panic rather than return a nil registry
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

// Registry defines a templates registry that is safe to be used by multiple goroutines.
//
// Use the Registry.Load* methods to load templates into the registry.
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
func (r *Registry) AddFuncs(funcs map[string]any) *Registry {
	maps.Copy(r.funcs, funcs)

	return r
}

// 缓存 key 带来源前缀：文件、字符串、FS 三种来源共用一个缓存，不加前缀的话，
// 模板文本恰好等于某个文件路径时会取到文件模板。%#v 保留了文件名之间的边界。
func filesKey(filenames []string) string { return fmt.Sprintf("files:%#v", filenames) }
func stringKey(text string) string       { return "string:" + text }
func fsKey(fsys fs.FS, patterns []string) string {
	return fmt.Sprintf("fs:%v%#v", fsys, patterns)
}

// LoadFiles caches (if not already) the specified filenames set as a
// single template and returns a ready to use Renderer instance.
//
// There must be at least 1 filename specified.
func (r *Registry) LoadFiles(filenames ...string) *Renderer {
	if len(filenames) == 0 {
		return &Renderer{parseError: errors.New("at least 1 filename is required")}
	}

	// 前缀把文件、字符串、FS 三种来源的 key 空间分开；%#v 保留了文件名之间的边界，
	// "a,b"（一个文件）和 "a","b"（两个文件）不会得到同一个 key
	key := filesKey(filenames)

	v, ok := r.cache.Get(key)
	if ok {
		return v
	}

	// parse and cache
	tpl, err := template.New(filepath.Base(filenames[0])).Funcs(r.funcs).ParseFiles(filenames...)
	v = &Renderer{template: tpl, parseError: err}
	r.cache.Add(key, v)

	return v
}

// LoadString caches (if not already) the specified inline string as a
// single template and returns a ready to use Renderer instance.
func (r *Registry) LoadString(text string) *Renderer {
	// 不能直接拿模板文本当 key：文本恰好等于某个文件名时，会命中 LoadFiles 缓存的文件模板
	key := stringKey(text)
	v, ok := r.cache.Get(key)
	if ok {
		return v
	}

	// parse and cache
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
func (r *Registry) LoadFS(fsys fs.FS, globPatterns ...string) *Renderer {
	// %#v keeps distinct pattern sets distinct: %v renders ["x y"] and ["x", "y"] identically
	key := fsKey(fsys, globPatterns)

	v, ok := r.cache.Get(key)
	if ok {
		return v
	}

	// find the first file to use as template name (it is required when specifying Funcs)
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
func (r *Registry) Reset() {
	r.cache.Purge()
}
