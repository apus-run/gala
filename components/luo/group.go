// Package luo provides a way to group routes and applies middleware to them.
// Works with the standard library's http.ServeMux.
package luo

import (
	"net/http"
	"regexp"
	"strings"
)

// Group represents a set of routes with associated middleware.
type Group struct {
	mux         *http.ServeMux                    // the underlying mux to register the routes to
	basePath    string                            // base path for the group
	middlewares []func(http.Handler) http.Handler // middlewares stack

	// optional custom 404 handler
	notFound http.HandlerFunc

	// root points to the root group for global middleware application.
	// for the root group, root == nil.
	root *Group

	// routesLocked indicates that routes have been registered on the root group
	// and no further root-level middlewares may be added.
	routesLocked bool

	// rootCount captures how many root middlewares were present when this group
	// was created. Used to avoid double-applying root middlewares for per-route wrapping.
	rootCount int
}

// New creates a new Group.
func New(mux *http.ServeMux) *Group {
	return &Group{mux: mux}
}

// Mount creates a new group with a specified base path.
func Mount(mux *http.ServeMux, basePath string) *Group {
	return &Group{mux: mux, basePath: basePath}
}

// ServeHTTP implements the http.Handler interface
func (g *Group) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// resolve the root group (where global middlewares live).
	root := g
	if g.root != nil {
		root = g.root
	}

	// get the handler and pattern for this request
	_, pattern := g.mux.Handler(r)

	// if a pattern was found, create a shallow copy of the request with the pattern set
	// this allows global middlewares to see the pattern before mux.ServeHTTP is called
	if pattern != "" {
		r2 := *r
		r2.Pattern = pattern
		r = &r2
	}

	// create a handler that will let the mux do its routing (including setting path parameters)
	// but intercept 404s to use custom handler if provided
	muxHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if pattern == "" && root.notFound != nil {
			handler, currentPattern := g.mux.Handler(r)
			if currentPattern != "" {
				g.mux.ServeHTTP(w, r)
				return
			}

			// no route matched, need to check if it's a true 404
			// probe the synthetic handler to see what status it would return
			probe := &statusRecorder{status: http.StatusOK}
			handler.ServeHTTP(probe, r)

			// let the mux handle redirects and method mismatches
			if probe.status != http.StatusNotFound {
				g.mux.ServeHTTP(w, r)
				return
			}

			// it's a true 404, use custom handler
			root.notFound.ServeHTTP(w, r)
			return
		}
		// let the mux handle the request normally (this sets path parameters)
		g.mux.ServeHTTP(w, r)
	})

	// apply root (global) middlewares around the mux handler and serve the request.
	root.wrapGlobal(muxHandler).ServeHTTP(w, r)
}

// Group creates a new group with the same middleware stack as the original on top of the existing group.
func (g *Group) Group() *Group {
	return g.clone() // copy the middlewares to avoid modifying the original
}

// Mount creates a new group with a specified base path on top of the existing group.
func (g *Group) Mount(basePath string) *Group {
	ng := g.clone() // copy the middlewares to avoid modifying the original
	ng.basePath += basePath
	return ng
}

// Use adds middleware(s) to the Group.
// Middlewares are executed in the order they are added.
// Note: Root-level middlewares (added to the root group) have access to the matched
// route pattern via r.Pattern, but execute before path parameters are parsed.
// Therefore, r.PathValue() will return empty strings in root middlewares.
// Middlewares on mounted groups execute after routing and have full access to path values.
func (g *Group) Use(middleware func(http.Handler) http.Handler, more ...func(http.Handler) http.Handler) {
	// disallow adding middlewares after any routes have been registered on this group.
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
func (g *Group) With(middleware func(http.Handler) http.Handler, more ...func(http.Handler) http.Handler) *Group {
	newMiddlewares := make([]func(http.Handler) http.Handler, len(g.middlewares), len(g.middlewares)+len(more)+1)
	copy(newMiddlewares, g.middlewares)
	newMiddlewares = append(newMiddlewares, middleware)
	newMiddlewares = append(newMiddlewares, more...)
	// preserve root pointer and rootCount
	ng := &Group{mux: g.mux, basePath: g.basePath, middlewares: newMiddlewares, root: g.root, rootCount: g.rootCount}
	if ng.root == nil {
		// g is the root, so all g's middlewares are root middlewares
		ng.root = g
		ng.rootCount = len(g.middlewares)
	}
	return ng
}

// Handle adds a new route to the Group's mux, applying all middlewares to the handler.
func (g *Group) Handle(pattern string, handler http.Handler) {
	g.lockRoot() // lock root on first route registration

	// for file server paths (ending with /), preserve the pattern as-is
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
func (g *Group) HandleFiles(pattern string, root http.FileSystem) {
	g.lockRoot() // lock root on first route registration

	// the method part must stay out of the path that gets the mount prefix and is stripped
	var method string
	if matches := reGo122.FindStringSubmatch(pattern); len(matches) > 2 {
		method = matches[1] + " "
		pattern = matches[2]
	}

	// normalize pattern to always have trailing slash
	if !strings.HasSuffix(pattern, "/") {
		pattern += "/"
	}

	// build the full path for registration
	fullPath := g.basePath + pattern

	if pattern == "/" && g.basePath == "" {
		// root case - serve directly without stripping
		g.mux.Handle(method+"/", g.wrapMiddleware(http.FileServer(root)))
		return
	}

	// for both mounted groups and prefixed paths, strip the fullPath
	handler := http.StripPrefix(strings.TrimSuffix(fullPath, "/"), http.FileServer(root))
	g.mux.Handle(method+fullPath, g.wrapMiddleware(handler))
}

// HandleFunc registers the handler function for the given pattern to the Group's mux.
// The handler is wrapped with the Group's middlewares.
func (g *Group) HandleFunc(pattern string, handler http.HandlerFunc) {
	g.register(pattern, handler)
}

// Handler returns the handler and the pattern that matches the request.
// It always returns a non-nil handler, see http.ServeMux.Handler documentation for details.
func (g *Group) Handler(r *http.Request) (h http.Handler, pattern string) {
	return g.mux.Handler(r)
}

// DisableNotFoundHandler has no effect.
//
// Deprecated: this method has no effect.
func (g *Group) DisableNotFoundHandler() {}

// NotFoundHandler sets a custom handler for true 404 responses.
// Method mismatches and path-cleanup redirects are handled by http.ServeMux.
func (g *Group) NotFoundHandler(handler http.HandlerFunc) {
	// always set on the root group so custom 404 works regardless of which group serves.
	if g.root != nil {
		g.root.notFound = handler
		return
	}
	g.notFound = handler
}

// matches non-space characters, spaces, then anything, i.e. "GET /path/to/resource"
var reGo122 = regexp.MustCompile(`^(\S+)\s+(.+)$`)

func (g *Group) register(pattern string, handler http.HandlerFunc) {
	g.lockRoot() // lock root on first route registration
	matches := reGo122.FindStringSubmatch(pattern)
	var path, method string
	if len(matches) > 2 { // path in the form "GET /path/to/resource"
		method = matches[1]
		path = matches[2]
		pattern = method + " " + g.basePath + path
	} else { // path is just "/path/to/resource"
		path = pattern
		pattern = g.basePath + pattern
		// method is not set intentionally here, the request pattern had no method part
	}
	// if the pattern is the root path on / change it to /{$}
	// this keeps handling the root request without becoming a catch-all
	if pattern == "/" || path == "/" {
		if method != "" { // preserve the method part if it was set
			pattern = method + " " + g.basePath + "/{$}"
		} else {
			pattern = g.basePath + "/{$}" // no method part, just the path
		}
	}
	g.mux.HandleFunc(pattern, g.wrapMiddleware(handler).ServeHTTP)
}

// Route allows for configuring the Group inside the configureFn function.
// When called on the root group, it automatically creates a new group to avoid
// accidentally modifying the root group's middleware stack.
func (g *Group) Route(configureFn func(*Group)) {
	// if called on root group, auto-create a group for better UX
	if g.root == nil {
		child := g.Group()
		configureFn(child)
		// if child registered routes, lock root too to prevent Use() after routes
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
func (g *Group) HandleRoot(method string, handler http.Handler) {
	g.lockRoot() // lock root on first route registration

	// for empty base path, use "/" to match the root
	pattern := g.basePath
	if pattern == "" {
		pattern = "/"
	}

	// add method if specified
	if method != "" {
		pattern = method + " " + pattern
	}

	g.mux.Handle(pattern, g.wrapMiddleware(handler))
}

// HandleRootFunc is like HandleRoot but takes a handler function.
func (g *Group) HandleRootFunc(method string, handler http.HandlerFunc) {
	g.lockRoot() // lock root on first route registration

	// for empty base path, use "/" to match the root
	pattern := g.basePath
	if pattern == "" {
		pattern = "/"
	}

	// add method if specified
	if method != "" {
		pattern = method + " " + pattern
	}

	g.mux.HandleFunc(pattern, g.wrapMiddleware(handler).ServeHTTP)
}

// wrapMiddleware applies the registered middlewares to a handler.
func (g *Group) wrapMiddleware(handler http.Handler) http.Handler {
	// root group: don't apply middlewares here, they're applied globally in ServeHTTP
	if g.root == nil {
		return handler
	}

	// child group: apply only middlewares added after mounting (exclude inherited root middlewares)
	start := g.rootCount
	if start > len(g.middlewares) {
		start = len(g.middlewares) // safety: ensure start doesn't exceed bounds
	}

	for i := len(g.middlewares) - 1; i >= start; i-- {
		handler = g.middlewares[i](handler)
	}
	return handler
}

func (g *Group) clone() *Group {
	middlewares := make([]func(http.Handler) http.Handler, len(g.middlewares))
	copy(middlewares, g.middlewares)
	// preserve root pointer and rootCount
	ng := &Group{mux: g.mux, basePath: g.basePath, middlewares: middlewares, root: g.root, rootCount: g.rootCount}
	if ng.root == nil {
		// g is the root, so all g's middlewares are root middlewares
		ng.root = g
		ng.rootCount = len(g.middlewares)
	}
	return ng
}

// Wrap directly wraps the handler with the provided middleware(s).
func Wrap(handler http.Handler, mw1 func(http.Handler) http.Handler, mws ...func(http.Handler) http.Handler) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		handler = mws[i](handler)
	}
	return mw1(handler) // apply the first middleware
}

// wrapGlobal applies only the root group's middlewares to the provided handler.
func (g *Group) wrapGlobal(handler http.Handler) http.Handler {
	// resolve root group
	root := g
	if g.root != nil {
		root = g.root
	}
	for i := len(root.middlewares) - 1; i >= 0; i-- {
		handler = root.middlewares[i](handler)
	}
	return handler
}

// lockRoot marks this group as having registered routes.
func (g *Group) lockRoot() { g.routesLocked = true }

// statusRecorder is a minimal ResponseWriter that only records the status code.
// Used to probe what status the mux would return without actually writing a response.
type statusRecorder struct {
	status int
}

func (r *statusRecorder) Header() http.Header {
	return make(http.Header)
}

func (r *statusRecorder) Write([]byte) (int, error) {
	return 0, nil
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
}
