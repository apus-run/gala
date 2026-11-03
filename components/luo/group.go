// Package luo groups routes and applies middleware on top of http.ServeMux | 在 http.ServeMux 之上做路由分组与中间件.
//
// # Notes | 说明
//
// Luo is a grouping layer, not a router: matching stays with the stdlib mux
// (Go 1.22 "METHOD /path/{id}" patterns). Root-group middlewares run before
// routing — they see r.Pattern but r.PathValue is empty there; middlewares on
// mounted groups run after routing with full path values. Calling Use after
// routes were registered on that group panics. A custom NotFoundHandler only
// covers true 404s; redirects and method mismatches stay with the mux.
// luo 是分组层而非路由器：匹配仍由标准库 mux 完成（Go 1.22 的
// "METHOD /path/{id}" 模式）。根组中间件在路由前执行——能看到 r.Pattern，
// 但 r.PathValue 为空；挂载组的中间件在路由后执行，可读路径参数。该组注册
// 过路由后再 Use 会 panic。自定义 NotFoundHandler 只覆盖真 404；重定向与
// 方法不匹配仍交给 mux。
//
// # Features | 功能
//
// - Mount/Group/With scoped middleware stacks | Mount/Group/With 作用域中间件
// - GET/POST/... method helpers and ANY | 各 HTTP 方法辅助与 ANY
// - HandleFiles and HandleRoot edge-case helpers | 静态目录与根路径辅助
// - Adapt error-returning handlers | Adapt 适配返回错误的处理器
//
// # Example | 示例
//
//	mux := http.NewServeMux()
//	g := luo.Mount(mux, "/api")
//	g.Use(auth)
//	g.GET("/users/{id}", getUser)
package luo

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/apus-run/gala/pkg/chain"
)

// Group represents a set of routes with associated middleware.
// Group 表示一组路由及其关联的中间件。
type Group struct {
	mux         *http.ServeMux                    // the underlying mux to register the routes to | 注册路由的底层 mux
	basePath    string                            // base path for the group | 组的基础路径
	middlewares []func(http.Handler) http.Handler // middlewares stack | 中间件栈

	// optional custom 404 handler | 可选的自定义 404 处理器
	notFound http.HandlerFunc

	// root points to the root group for global middleware application.
	// for the root group, root == nil.
	// root 指向根组以应用全局中间件；根组的 root == nil。
	root *Group

	// routesLocked indicates that routes have been registered on the root group
	// and no further root-level middlewares may be added.
	// routesLocked 表示根组已注册路由，不能再添加根级中间件。
	routesLocked bool

	// rootCount captures how many root middlewares were present when this group
	// was created. Used to avoid double-applying root middlewares for per-route wrapping.
	// rootCount 记录创建本组时已有的根中间件数量，避免按路由包装时重复应用根中间件。
	rootCount int
}

// New creates a new Group.
// New 创建新的 Group。
func New(mux *http.ServeMux) *Group {
	return &Group{mux: mux}
}

// Mount creates a new group with a specified base path.
// Mount 以指定基础路径创建新组。
func Mount(mux *http.ServeMux, basePath string) *Group {
	return &Group{mux: mux, basePath: basePath}
}

// ServeHTTP implements the http.Handler interface
// ServeHTTP 实现 http.Handler 接口。
func (g *Group) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// resolve the root group (where global middlewares live).
	// 解析根组（全局中间件所在处）。
	root := g
	if g.root != nil {
		root = g.root
	}

	// get the handler and pattern for this request
	// 获取该请求的处理器与模式
	_, pattern := g.mux.Handler(r)

	// if a pattern was found, create a shallow copy of the request with the pattern set
	// this allows global middlewares to see the pattern before mux.ServeHTTP is called
	// 命中模式时浅拷贝请求并写入 pattern，让全局中间件在 mux.ServeHTTP 之前看到它
	if pattern != "" {
		r2 := *r
		r2.Pattern = pattern
		r = &r2
	}

	// create a handler that will let the mux do its routing (including setting path parameters)
	// but intercept 404s to use custom handler if provided
	// 构建一个 handler：让 mux 完成路由（含路径参数解析），但拦截 404 以便使用自定义处理器
	muxHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if pattern == "" && root.notFound != nil {
			handler, currentPattern := g.mux.Handler(r)
			if currentPattern != "" {
				g.mux.ServeHTTP(w, r)
				return
			}

			// no route matched, need to check if it's a true 404
			// probe the synthetic handler to see what status it would return
			// 没有路由匹配，需要判断是否真 404：
			// 用合成 handler 探测它将返回的状态码
			probe := &statusRecorder{status: http.StatusOK}
			handler.ServeHTTP(probe, r)

			// let the mux handle redirects and method mismatches
			// 重定向与方法不匹配仍交给 mux 处理
			if probe.status != http.StatusNotFound {
				g.mux.ServeHTTP(w, r)
				return
			}

			// it's a true 404, use custom handler
			// 是真 404，使用自定义处理器
			root.notFound.ServeHTTP(w, r)
			return
		}
		// let the mux handle the request normally (this sets path parameters)
		// 让 mux 正常处理请求（此处设置路径参数）
		g.mux.ServeHTTP(w, r)
	})

	// apply root (global) middlewares around the mux handler and serve the request.
	// 在 mux handler 外层套上根（全局）中间件并处理请求。
	root.wrapGlobal(muxHandler).ServeHTTP(w, r)
}

// Group creates a new group with the same middleware stack as the original on top of the existing group.
// Group 在现有组之上创建继承原中间件栈的新组。
func (g *Group) Group() *Group {
	return g.clone() // copy the middlewares to avoid modifying the original | 复制中间件以免修改原组
}

// Mount creates a new group with a specified base path on top of the existing group.
// Mount 在现有组之上以指定基础路径创建新组。
func (g *Group) Mount(basePath string) *Group {
	ng := g.clone() // copy the middlewares to avoid modifying the original | 复制中间件以免修改原组
	ng.basePath += basePath
	return ng
}

// Use adds middleware(s) to the Group.
// Middlewares are executed in the order they are added.
// Note: Root-level middlewares (added to the root group) have access to the matched
// route pattern via r.Pattern, but execute before path parameters are parsed.
// Therefore, r.PathValue() will return empty strings in root middlewares.
// Middlewares on mounted groups execute after routing and have full access to path values.
// Use 向组添加中间件，按添加顺序执行。注意：根级中间件可通过 r.Pattern 看到
// 匹配的路由模式，但在路径参数解析之前执行，因此 r.PathValue() 在根中间件中
// 返回空串；挂载组的中间件在路由后执行，可读取全部路径值。
func (g *Group) Use(middleware func(http.Handler) http.Handler, more ...func(http.Handler) http.Handler) {
	// disallow adding middlewares after any routes have been registered on this group.
	// 该组注册过路由后不允许再加中间件。
	if g.routesLocked {
		panic("luo: Use called after routes were registered on this group; add middlewares before registering routes or use Group/With for scoped middleware")
	}
	g.middlewares = append(g.middlewares, middleware)
	g.middlewares = append(g.middlewares, more...)
}

// With adds new middleware(s) to the Group and returns a new Group with the updated middleware stack.
// The With method is similar to Use, but instead of modifying the current Group,
// it returns a new Group instance with the added middleware(s).
// This allows for creating chain of middleware without affecting the original Group.
// With 向组添加新中间件并返回携带更新后中间件栈的新组。与 Use 类似，但不是
// 修改当前组，而是返回新增中间件后的新组实例，便于构建中间件链而不影响原组。
func (g *Group) With(middleware func(http.Handler) http.Handler, more ...func(http.Handler) http.Handler) *Group {
	newMiddlewares := make([]func(http.Handler) http.Handler, len(g.middlewares), len(g.middlewares)+len(more)+1)
	copy(newMiddlewares, g.middlewares)
	newMiddlewares = append(newMiddlewares, middleware)
	newMiddlewares = append(newMiddlewares, more...)
	// preserve root pointer and rootCount | 保留 root 指针与 rootCount
	ng := &Group{mux: g.mux, basePath: g.basePath, middlewares: newMiddlewares, root: g.root, rootCount: g.rootCount}
	if ng.root == nil {
		// g is the root, so all g's middlewares are root middlewares
		// g 即根组，其所有中间件都是根中间件
		ng.root = g
		ng.rootCount = len(g.middlewares)
	}
	return ng
}

// Handle adds a new route to the Group's mux, applying all middlewares to the handler.
// Handle 向组的 mux 注册新路由，并给 handler 应用全部中间件。
func (g *Group) Handle(pattern string, handler http.Handler) {
	g.lockRoot() // lock root on first route registration | 首次注册路由时锁定根组

	// for file server paths (ending with /), preserve the pattern as-is
	// 对文件服务路径（以 / 结尾）原样保留模式
	if strings.HasSuffix(pattern, "/") {
		fullPath := g.basePath + pattern
		if matches := reGo122.FindStringSubmatch(pattern); len(matches) > 2 {
			fullPath = matches[1] + " " + g.basePath + matches[2]
		}
		g.mux.Handle(fullPath, g.wrapMiddleware(handler))
		return
	}
	g.register(pattern, handler.ServeHTTP)
}

// HandleFiles is a helper to serve static files from a directory
// HandleFiles 是从目录提供静态文件的辅助函数。
func (g *Group) HandleFiles(pattern string, root http.FileSystem) {
	g.lockRoot() // lock root on first route registration | 首次注册路由时锁定根组

	// the method part must stay out of the path that gets the mount prefix and is stripped
	// method 部分必须留在路径之外：该路径会被加上挂载前缀并被 strip
	var method string
	if matches := reGo122.FindStringSubmatch(pattern); len(matches) > 2 {
		method = matches[1] + " "
		pattern = matches[2]
	}

	// normalize pattern to always have trailing slash
	// 把模式规范成始终带尾斜杠
	if !strings.HasSuffix(pattern, "/") {
		pattern += "/"
	}

	// build the full path for registration
	// 构建注册用的完整路径
	fullPath := g.basePath + pattern

	if pattern == "/" && g.basePath == "" {
		// root case - serve directly without stripping
		// 根路径场景——直接服务，不做 strip
		g.mux.Handle(method+"/", g.wrapMiddleware(http.FileServer(root)))
		return
	}

	// for both mounted groups and prefixed paths, strip the fullPath
	// 挂载组与前缀路径都需要 strip 掉 fullPath
	handler := http.StripPrefix(strings.TrimSuffix(fullPath, "/"), http.FileServer(root))
	g.mux.Handle(method+fullPath, g.wrapMiddleware(handler))
}

// HandleFunc registers the handler function for the given pattern to the Group's mux.
// The handler is wrapped with the Group's middlewares.
// HandleFunc 把处理函数按给定模式注册到组的 mux；handler 会被组的中间件包装。
func (g *Group) HandleFunc(pattern string, handler http.HandlerFunc) {
	g.register(pattern, handler)
}

// Handler returns the handler and the pattern that matches the request.
// It always returns a non-nil handler, see http.ServeMux.Handler documentation for details.
// Handler 返回匹配请求的处理器与模式；始终返回非 nil 处理器，
// 详见 http.ServeMux.Handler 文档。
func (g *Group) Handler(r *http.Request) (h http.Handler, pattern string) {
	return g.mux.Handler(r)
}

// DisableNotFoundHandler has no effect.
//
// Deprecated: this method has no effect.
// DisableNotFoundHandler 无任何效果。
//
// 已废弃：该方法没有效果。
func (g *Group) DisableNotFoundHandler() {}

// NotFoundHandler sets a custom handler for true 404 responses.
// Method mismatches and path-cleanup redirects are handled by http.ServeMux.
// NotFoundHandler 为真 404 响应设置自定义处理器；方法不匹配与路径清理重定向
// 仍由 http.ServeMux 处理。
func (g *Group) NotFoundHandler(handler http.HandlerFunc) {
	// always set on the root group so custom 404 works regardless of which group serves.
	// 始终设置在根组上，保证无论哪个组服务都能生效。
	if g.root != nil {
		g.root.notFound = handler
		return
	}
	g.notFound = handler
}

// matches non-space characters, spaces, then anything, i.e. "GET /path/to/resource"
// 匹配"非空白字符 + 空白 + 其余部分"，即 "GET /path/to/resource"
var reGo122 = regexp.MustCompile(`^(\S+)\s+(.+)$`)

func (g *Group) register(pattern string, handler http.HandlerFunc) {
	g.lockRoot() // lock root on first route registration | 首次注册路由时锁定根组
	matches := reGo122.FindStringSubmatch(pattern)
	var path, method string
	if len(matches) > 2 { // path in the form "GET /path/to/resource" | 形如 "GET /path/to/resource" 的路径
		method = matches[1]
		path = matches[2]
		pattern = method + " " + g.basePath + path
	} else { // path is just "/path/to/resource" | 路径仅为 "/path/to/resource"
		path = pattern
		pattern = g.basePath + pattern
		// method is not set intentionally here, the request pattern had no method part
		// 有意不设置 method：请求模式本身没有方法部分
	}
	// if the pattern is the root path on / change it to /{$}
	// this keeps handling the root request without becoming a catch-all
	// 根路径 / 改写成 /{$}：既继续处理根请求，又不会变成全捕获
	if pattern == "/" || path == "/" {
		if method != "" { // preserve the method part if it was set | 有方法部分则保留
			pattern = method + " " + g.basePath + "/{$}"
		} else {
			pattern = g.basePath + "/{$}" // no method part, just the path | 无方法部分，仅路径
		}
	}
	g.mux.HandleFunc(pattern, g.wrapMiddleware(handler).ServeHTTP)
}

// Route allows for configuring the Group inside the configureFn function.
// When called on the root group, it automatically creates a new group to avoid
// accidentally modifying the root group's middleware stack.
// Route 在 configureFn 内配置组。在根组上调用时会自动创建子组，
// 避免误改根组的中间件栈。
func (g *Group) Route(configureFn func(*Group)) {
	// if called on root group, auto-create a group for better UX
	// 在根组上调用时自动创建子组，体验更好
	if g.root == nil {
		child := g.Group()
		configureFn(child)
		// if child registered routes, lock root too to prevent Use() after routes
		// 子组注册过路由则同样锁定根组，防止路由之后再 Use()
		if child.routesLocked {
			g.routesLocked = true
		}
		return
	}
	configureFn(g)
}

// HandleRoot adds a handler for the group's root path without trailing slash.
// This avoids the 301 redirect that would occur with a "/" pattern.
// Method parameter can be empty to register for all HTTP methods.
// HandleRoot 为组的根路径（无尾斜杠）添加处理器，避免 "/" 模式产生的 301 重定向。
// method 为空时对所有 HTTP 方法生效。
func (g *Group) HandleRoot(method string, handler http.Handler) {
	g.lockRoot() // lock root on first route registration | 首次注册路由时锁定根组

	// for empty base path, use "/" to match the root
	// 基础路径为空时用 "/" 匹配根
	pattern := g.basePath
	if pattern == "" {
		pattern = "/"
	}

	// add method if specified
	// 指定了 method 则追加
	if method != "" {
		pattern = method + " " + pattern
	}

	g.mux.Handle(pattern, g.wrapMiddleware(handler))
}

// HandleRootFunc is like HandleRoot but takes a handler function.
// HandleRootFunc 与 HandleRoot 类似，但接收处理函数。
func (g *Group) HandleRootFunc(method string, handler http.HandlerFunc) {
	g.lockRoot() // lock root on first route registration | 首次注册路由时锁定根组

	// for empty base path, use "/" to match the root
	// 基础路径为空时用 "/" 匹配根
	pattern := g.basePath
	if pattern == "" {
		pattern = "/"
	}

	// add method if specified
	// 指定了 method 则追加
	if method != "" {
		pattern = method + " " + pattern
	}

	g.mux.HandleFunc(pattern, g.wrapMiddleware(handler).ServeHTTP)
}

// wrapMiddleware applies the registered middlewares to a handler.
// wrapMiddleware 给 handler 应用本组注册的中间件。
func (g *Group) wrapMiddleware(handler http.Handler) http.Handler {
	// root group: don't apply middlewares here, they're applied globally in ServeHTTP
	// 根组：此处不应用中间件，它们在 ServeHTTP 中全局应用
	if g.root == nil {
		return handler
	}

	// child group: apply only middlewares added after mounting (exclude inherited root middlewares)
	// Ensure start doesn't exceed bounds.
	// 子组：只应用挂载之后新增的中间件（排除继承的根中间件），并保证 start 不越界
	start := min(g.rootCount, len(g.middlewares))

	if start == len(g.middlewares) {
		return handler
	}
	return chain.Chain(g.middlewares[start], g.middlewares[start+1:]...)(handler)
}

func (g *Group) clone() *Group {
	middlewares := make([]func(http.Handler) http.Handler, len(g.middlewares))
	copy(middlewares, g.middlewares)
	// preserve root pointer and rootCount | 保留 root 指针与 rootCount
	ng := &Group{mux: g.mux, basePath: g.basePath, middlewares: middlewares, root: g.root, rootCount: g.rootCount}
	if ng.root == nil {
		// g is the root, so all g's middlewares are root middlewares
		// g 即根组，其所有中间件都是根中间件
		ng.root = g
		ng.rootCount = len(g.middlewares)
	}
	return ng
}

// Wrap directly wraps the handler with the provided middleware(s).
// Wrap 直接用给定中间件包装 handler。
func Wrap(handler http.Handler, mw1 func(http.Handler) http.Handler, mws ...func(http.Handler) http.Handler) http.Handler {
	return chain.Chain(mw1, mws...)(handler)
}

// wrapGlobal applies only the root group's middlewares to the provided handler.
// wrapGlobal 只把根组的中间件应用到给定 handler。
func (g *Group) wrapGlobal(handler http.Handler) http.Handler {
	// resolve root group | 解析根组
	root := g
	if g.root != nil {
		root = g.root
	}
	if len(root.middlewares) == 0 {
		return handler
	}
	return chain.Chain(root.middlewares[0], root.middlewares[1:]...)(handler)
}

// lockRoot marks this group as having registered routes.
// lockRoot 标记该组已注册路由。
func (g *Group) lockRoot() { g.routesLocked = true }

// statusRecorder is a minimal ResponseWriter that only records the status code.
// Used to probe what status the mux would return without actually writing a response.
// statusRecorder 是只记录状态码的最小 ResponseWriter，
// 用于在不真正写出响应的情况下探测 mux 将返回的状态。
type statusRecorder struct {
	status int
}

// Header implements http.Handler's ResponseWriter contract | 实现 http.ResponseWriter 的 Header.
func (r *statusRecorder) Header() http.Header {
	return make(http.Header)
}

// Write implements http.ResponseWriter, discarding the bytes | 实现 http.ResponseWriter，丢弃写入的字节.
func (r *statusRecorder) Write([]byte) (int, error) {
	return 0, nil
}

// WriteHeader implements http.ResponseWriter, recording the status | 实现 http.ResponseWriter，记录状态码.
func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
}
