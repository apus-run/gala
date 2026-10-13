package luo

import "net/http"

// Method registers a method-specific route through HandleFunc, preserving group
// prefixes, middleware and root-path semantics. An empty method matches all
// methods. Custom methods such as PROPFIND are supported by http.ServeMux.
func (g *Group) Method(method, pattern string, handler http.HandlerFunc) {
	if method != "" {
		pattern = method + " " + pattern
	}
	g.HandleFunc(pattern, handler)
}

// GET registers a GET route. ServeMux also matches HEAD unless explicitly registered.
func (g *Group) GET(pattern string, handler http.HandlerFunc) {
	g.Method(http.MethodGet, pattern, handler)
}

// POST registers a POST route.
func (g *Group) POST(pattern string, handler http.HandlerFunc) {
	g.Method(http.MethodPost, pattern, handler)
}

// PUT registers a PUT route.
func (g *Group) PUT(pattern string, handler http.HandlerFunc) {
	g.Method(http.MethodPut, pattern, handler)
}

// PATCH registers a PATCH route.
func (g *Group) PATCH(pattern string, handler http.HandlerFunc) {
	g.Method(http.MethodPatch, pattern, handler)
}

// DELETE registers a DELETE route.
func (g *Group) DELETE(pattern string, handler http.HandlerFunc) {
	g.Method(http.MethodDelete, pattern, handler)
}

// HEAD registers a HEAD route, taking precedence over a matching GET route.
func (g *Group) HEAD(pattern string, handler http.HandlerFunc) {
	g.Method(http.MethodHead, pattern, handler)
}

// OPTIONS registers an OPTIONS route.
func (g *Group) OPTIONS(pattern string, handler http.HandlerFunc) {
	g.Method(http.MethodOptions, pattern, handler)
}

// CONNECT registers a CONNECT route.
func (g *Group) CONNECT(pattern string, handler http.HandlerFunc) {
	g.Method(http.MethodConnect, pattern, handler)
}

// TRACE registers a TRACE route.
func (g *Group) TRACE(pattern string, handler http.HandlerFunc) {
	g.Method(http.MethodTrace, pattern, handler)
}

// ANY registers a route without a method constraint, including custom methods.
// A matching method-specific route takes precedence.
func (g *Group) ANY(pattern string, handler http.HandlerFunc) {
	g.Method("", pattern, handler)
}
