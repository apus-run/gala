// Package casbin adapts authenticated Gin requests to an authorization interface.
// Install a real authentication middleware before this handler.
package casbin

import (
	"errors"
	"net/http"
	"reflect"

	"github.com/gin-gonic/gin"
)

// Enforcer is the authorization capability required by the middleware. Its
// implementation must be safe for concurrent requests and is owned by the app.
type Enforcer interface{ Enforce(...any) (bool, error) }

// Builder assembles an authorization middleware using chainable setters.
// Configure it before Build; setters and Build are not safe to call concurrently.
// Built handlers capture a copy and are unaffected by later builder changes.
// Callbacks run on the request goroutine and must not retain gin.Context.
// Lookup must read an identity established by preceding authentication.
type Builder struct {
	enforcer Enforcer
	// Lookup returns ("", nil) for no identity, and a non-nil error for failure.
	lookup func(*gin.Context) (string, error)
	skip   func(*gin.Context) bool
	// RequestValues defaults to subject, URL.Path and HTTP method.
	requestValues func(*gin.Context, string) ([]any, error)
	unauthorized  gin.HandlerFunc
	forbidden     gin.HandlerFunc
	// ErrorHandler receives internal errors after the chain has been aborted.
	errorHandler func(*gin.Context, error)
}

// NewBuilder creates a builder. Enforcer and Lookup must be set before Build.
func NewBuilder() *Builder { return &Builder{} }

// SetEnforcer sets the application-owned, concurrency-safe authorizer.
func (b *Builder) SetEnforcer(value Enforcer) *Builder {
	b.enforcer = value
	return b
}

// SetLookup sets the trusted identity lookup; ("", nil) means no identity.
func (b *Builder) SetLookup(value func(*gin.Context) (string, error)) *Builder {
	b.lookup = value
	return b
}

// SetSkip sets an explicit bypass predicate; nil skips no requests.
func (b *Builder) SetSkip(value func(*gin.Context) bool) *Builder {
	b.skip = value
	return b
}

// SetRequestValues sets request extraction; nil uses subject, URL.Path and method.
func (b *Builder) SetRequestValues(value func(*gin.Context, string) ([]any, error)) *Builder {
	b.requestValues = value
	return b
}

// SetUnauthorized sets the missing-identity response; nil uses HTTP 401.
func (b *Builder) SetUnauthorized(value gin.HandlerFunc) *Builder {
	b.unauthorized = value
	return b
}

// SetForbidden sets the denied-access response; nil uses HTTP 403.
func (b *Builder) SetForbidden(value gin.HandlerFunc) *Builder {
	b.forbidden = value
	return b
}

// SetErrorHandler sets the internal-error response; nil uses HTTP 500 without a body.
func (b *Builder) SetErrorHandler(value func(*gin.Context, error)) *Builder {
	b.errorHandler = value
	return b
}

// Build is the route-authorization shorthand for RoutePermission.
func (b *Builder) Build() (gin.HandlerFunc, error) { return b.RoutePermission() }

// handler centralizes identity lookup and HTTP responses for all authorization
// modes. Empty requirement lists intentionally bypass identity lookup, matching
// the reference middleware; authentication remains a separate middleware.
func (cfg Builder) handler(empty bool, check func(*gin.Context, string) (bool, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		if empty || cfg.skip(c) {
			c.Next()
			return
		}
		subject, err := cfg.lookup(c)
		if err != nil {
			c.Abort()
			cfg.errorHandler(c, err)
			return
		}
		if subject == "" {
			c.Abort()
			cfg.unauthorized(c)
			return
		}
		allowed, err := check(c, subject)
		if err != nil {
			c.Abort()
			cfg.errorHandler(c, err)
			return
		}
		if !allowed {
			c.Abort()
			cfg.forbidden(c)
			return
		}
		c.Next()
	}
}

func (cfg Builder) normalized() (Builder, error) {
	if cfg.enforcer == nil {
		return cfg, errors.New("casbin middleware: Enforcer is required")
	}
	if isNil(cfg.enforcer) {
		return cfg, errors.New("casbin middleware: typed nil Enforcer")
	}
	if cfg.lookup == nil {
		return cfg, errors.New("casbin middleware: Lookup is required")
	}
	if cfg.skip == nil {
		cfg.skip = func(*gin.Context) bool { return false }
	}
	if cfg.requestValues == nil {
		cfg.requestValues = func(c *gin.Context, s string) ([]any, error) {
			return []any{s, c.Request.URL.Path, c.Request.Method}, nil
		}
	}
	if cfg.unauthorized == nil {
		cfg.unauthorized = func(c *gin.Context) { c.Status(http.StatusUnauthorized) }
	}
	if cfg.forbidden == nil {
		cfg.forbidden = func(c *gin.Context) { c.Status(http.StatusForbidden) }
	}
	if cfg.errorHandler == nil {
		cfg.errorHandler = func(c *gin.Context, _ error) { c.Status(http.StatusInternalServerError) }
	}
	return cfg, nil
}

func isNil(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan, reflect.Interface:
		return v.IsNil()
	}
	return false
}
