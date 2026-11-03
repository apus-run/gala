package luo

import "net/http"

// Method registers a method-specific route through HandleFunc, preserving group
// prefixes, middleware and root-path semantics. An empty method matches all
// methods. Custom methods such as PROPFIND are supported by http.ServeMux.
// Method 经 HandleFunc 注册指定方法的路由，保留组前缀、中间件与根路径语义。
// method 为空匹配所有方法；http.ServeMux 也支持 PROPFIND 等自定义方法。
func (g *Group) Method(method, pattern string, handler http.HandlerFunc) {
	if method != "" {
		pattern = method + " " + pattern
	}
	g.HandleFunc(pattern, handler)
}

// GET registers a GET route. ServeMux also matches HEAD unless explicitly registered.
// GET 注册 GET 路由；未显式注册 HEAD 时 ServeMux 也会匹配 HEAD。
func (g *Group) GET(pattern string, handler http.HandlerFunc) {
	g.Method(http.MethodGet, pattern, handler)
}

// POST registers a POST route.
// POST 注册 POST 路由。
func (g *Group) POST(pattern string, handler http.HandlerFunc) {
	g.Method(http.MethodPost, pattern, handler)
}

// PUT registers a PUT route.
// PUT 注册 PUT 路由。
func (g *Group) PUT(pattern string, handler http.HandlerFunc) {
	g.Method(http.MethodPut, pattern, handler)
}

// PATCH registers a PATCH route.
// PATCH 注册 PATCH 路由。
func (g *Group) PATCH(pattern string, handler http.HandlerFunc) {
	g.Method(http.MethodPatch, pattern, handler)
}

// DELETE registers a DELETE route.
// DELETE 注册 DELETE 路由。
func (g *Group) DELETE(pattern string, handler http.HandlerFunc) {
	g.Method(http.MethodDelete, pattern, handler)
}

// HEAD registers a HEAD route, taking precedence over a matching GET route.
// HEAD 注册 HEAD 路由，优先于匹配的 GET 路由。
func (g *Group) HEAD(pattern string, handler http.HandlerFunc) {
	g.Method(http.MethodHead, pattern, handler)
}

// OPTIONS registers an OPTIONS route.
// OPTIONS 注册 OPTIONS 路由。
func (g *Group) OPTIONS(pattern string, handler http.HandlerFunc) {
	g.Method(http.MethodOptions, pattern, handler)
}

// CONNECT registers a CONNECT route.
// CONNECT 注册 CONNECT 路由。
func (g *Group) CONNECT(pattern string, handler http.HandlerFunc) {
	g.Method(http.MethodConnect, pattern, handler)
}

// TRACE registers a TRACE route.
// TRACE 注册 TRACE 路由。
func (g *Group) TRACE(pattern string, handler http.HandlerFunc) {
	g.Method(http.MethodTrace, pattern, handler)
}

// ANY registers a route without a method constraint, including custom methods.
// A matching method-specific route takes precedence.
// ANY 注册不限方法的路由（含自定义方法）；匹配的指定方法路由优先。
func (g *Group) ANY(pattern string, handler http.HandlerFunc) {
	g.Method("", pattern, handler)
}
