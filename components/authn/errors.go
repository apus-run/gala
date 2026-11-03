package authn

import (
	"errors"
	"net/http"
)

// ErrMissingToken indicates that the request did not contain a usable Bearer
// token. It is deliberately distinct from token validation failures.
// ErrMissingToken 表示请求里没有可用的 Bearer 令牌。
// 它与令牌校验失败刻意区分开。
var ErrMissingToken = errors.New("authn: missing bearer token")

// ErrSessionClaimsUnsupported is returned when WithClaims is passed to
// NewSessionAuth. SessionAuth owns and validates its own SessionClaims type.
// ErrSessionClaimsUnsupported 在 WithClaims 传给 NewSessionAuth 时返回。
// SessionAuth 自己拥有并校验其 SessionClaims 类型。
var ErrSessionClaimsUnsupported = errors.New("authn: SessionAuth does not support WithClaims")

// StatusOf maps an authentication error to an HTTP status code and an optional
// WWW-Authenticate challenge. Only errors caused by the client's credentials
// are mapped to 401; infrastructure failures fail closed with 503.
// StatusOf 把认证错误映射为 HTTP 状态码和可选的 WWW-Authenticate 质询。
// 只有客户端凭据导致的错误才映射为 401；基础设施故障一律失败关闭返回 503。
func StatusOf(err error) (code int, challenge string) {
	switch {
	case errors.Is(err, ErrMissingToken):
		return http.StatusUnauthorized, "Bearer"
	case errors.Is(err, ErrTokenInvalid), errors.Is(err, ErrSessionInvalid):
		return http.StatusUnauthorized, `Bearer error="invalid_token"`
	default:
		return http.StatusServiceUnavailable, ""
	}
}
