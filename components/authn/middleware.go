package authn

import (
	"context"
	"net/http"
)

// AuthFunc authenticates an HTTP request and returns its identity. The
// identity type is generic so the middleware can also be used by API-key or
// other authentication schemes.
// AuthFunc 认证一个 HTTP 请求并返回其身份。身份类型是泛型的，
// 中间件因此也能用于 API-key 等其他认证方案。
type AuthFunc[T any] func(ctx context.Context, r *http.Request) (T, error)

// Middleware authenticates requests with an AuthFunc and stores the resulting
// identity in the request context for downstream handlers.
// Middleware 用 AuthFunc 认证请求，并把得到的身份存进请求 context 供下游 handler 使用。
type Middleware[T any] struct {
	auth    AuthFunc[T]
	onError func(http.ResponseWriter, *http.Request, error)
}

type middlewareOptions struct {
	onError func(http.ResponseWriter, *http.Request, error)
}

// MiddlewareOption customizes Middleware behavior | MiddlewareOption 自定义 Middleware 行为.
type MiddlewareOption interface {
	apply(*middlewareOptions)
}

// MiddlewareOptionFunc adapts a plain function to MiddlewareOption | 把普通函数适配成 MiddlewareOption.
type MiddlewareOptionFunc func(*middlewareOptions)

func (f MiddlewareOptionFunc) apply(o *middlewareOptions) {
	f(o)
}

// WithErrorHandler replaces the default auth error response. The default maps
// errors with StatusOf, sets WWW-Authenticate when applicable, and writes the
// corresponding HTTP status.
// WithErrorHandler 替换默认的认证错误响应。默认实现用 StatusOf 映射错误，
// 在适用时设置 WWW-Authenticate，并写入对应的 HTTP 状态码。
func WithErrorHandler(fn func(http.ResponseWriter, *http.Request, error)) MiddlewareOption {
	return MiddlewareOptionFunc(func(o *middlewareOptions) { o.onError = fn })
}

// NewMiddleware creates an authentication middleware | NewMiddleware 创建一个认证中间件.
func NewMiddleware[T any](auth AuthFunc[T], opts ...MiddlewareOption) *Middleware[T] {
	o := middlewareOptions{onError: defaultAuthError}
	for _, opt := range opts {
		opt.apply(&o)
	}
	return &Middleware[T]{auth: auth, onError: o.onError}
}

// Wrap returns an http.Handler that authenticates before calling next
// | Wrap 返回先认证再调用 next 的 http.Handler.
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
// AuthFunc 返回由 ParseAccess 支撑的认证函数，后者校验令牌用途与当前会话代次。
//
// Sources are tried in order and the first one that finds a token is used;
// without sources only the Authorization Bearer header is read. A token that
// is found but invalid fails the request: later sources are not tried, so a
// bad header can never fall back to an ambient cookie.
// sources 按顺序尝试，第一个找到令牌的生效；不传 sources 时只读 Authorization
// Bearer 头。找到但无效的令牌会直接失败：不会再尝试后面的 source，因此坏的头
// 永远不会回退到环境里的 cookie。
//
//	s.AuthFunc()                                              // API clients
//	s.AuthFunc(authn.TokenFromHeader, authn.TokenFromCookie("access")) // + browsers
func (s *SessionAuth) AuthFunc(sources ...TokenSource) AuthFunc[*SessionClaims] {
	if len(sources) == 0 {
		sources = []TokenSource{TokenFromHeader}
	}
	// the caller may reuse its slice, so copy before appending
	// 调用方可能复用自己的 slice，追加前先拷贝
	sources = append([]TokenSource(nil), sources...)
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
