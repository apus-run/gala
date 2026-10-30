package authn

import (
	"context"
	"net/http"
)

// AuthFunc authenticates an HTTP request and returns its identity. The
// identity type is generic so the middleware can also be used by API-key or
// other authentication schemes.
type AuthFunc[T any] func(ctx context.Context, r *http.Request) (T, error)

// Middleware authenticates requests with an AuthFunc and stores the resulting
// identity in the request context for downstream handlers.
type Middleware[T any] struct {
	auth    AuthFunc[T]
	onError func(http.ResponseWriter, *http.Request, error)
}

type middlewareOptions struct {
	onError func(http.ResponseWriter, *http.Request, error)
}

// MiddlewareOption customizes Middleware behavior.
type MiddlewareOption func(*middlewareOptions)

// WithErrorHandler replaces the default auth error response. The default maps
// errors with StatusOf, sets WWW-Authenticate when applicable, and writes the
// corresponding HTTP status.
func WithErrorHandler(fn func(http.ResponseWriter, *http.Request, error)) MiddlewareOption {
	return func(o *middlewareOptions) { o.onError = fn }
}

// NewMiddleware creates an authentication middleware.
func NewMiddleware[T any](auth AuthFunc[T], opts ...MiddlewareOption) *Middleware[T] {
	o := middlewareOptions{onError: defaultAuthError}
	for _, opt := range opts {
		opt(&o)
	}
	return &Middleware[T]{auth: auth, onError: o.onError}
}

// Wrap returns an http.Handler that authenticates before calling next.
func (m *Middleware[T]) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := m.auth(r.Context(), r)
		if err != nil {
			m.onError(w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(NewContext(r.Context(), id)))
	})
}

// AuthFunc returns an authentication function backed by ParseAccess, which
// validates token use and the current session generation.
//
// sources are tried in order and the first one that finds a token is used;
// without sources only the Authorization Bearer header is read. A token that is
// found but invalid fails the request: later sources are not tried, so a bad
// header can never fall back to an ambient cookie.
//
//	s.AuthFunc()                                              // API clients
//	s.AuthFunc(authn.TokenFromHeader, authn.TokenFromCookie("access")) // + browsers
func (s *SessionAuth) AuthFunc(sources ...TokenSource) AuthFunc[*SessionClaims] {
	if len(sources) == 0 {
		sources = []TokenSource{TokenFromHeader}
	}
	sources = append([]TokenSource(nil), sources...) // caller may reuse its slice
	return func(ctx context.Context, r *http.Request) (*SessionClaims, error) {
		raw, ok := firstToken(r, sources)
		if !ok {
			return nil, ErrMissingToken
		}
		return s.ParseAccess(ctx, raw)
	}
}

func defaultAuthError(w http.ResponseWriter, _ *http.Request, err error) {
	code, challenge := StatusOf(err)
	if challenge != "" {
		w.Header().Set("WWW-Authenticate", challenge)
	}
	http.Error(w, http.StatusText(code), code)
}
