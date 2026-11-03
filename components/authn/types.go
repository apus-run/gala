// Package authn issues, parses and revokes JWT tokens and guards HTTP
// requests with a generic middleware | 签发、解析、吊销 JWT 令牌，并用通用中间件守护 HTTP 请求。
//
// # Notes | 说明
//
// Two implementations of [Authenticator]. [JwtAuth] signs caller-supplied
// claims; revocation needs a store — without one, parsing skips the
// revocation list and Destroy returns ErrEmptySessionStore. [SessionAuth]
// issues rotating access/refresh pairs backed by a mandatory session store
// and does not support WithClaims. Signing and verification are delegated to
// components/token; business permissions stay with the caller.
// [Authenticator] 有两个实现：[JwtAuth] 签发调用方自定义的 claims，吊销需要
// 存储——没有存储时解析跳过吊销名单，Destroy 返回 ErrEmptySessionStore；
// [SessionAuth] 签发可轮换的访问/刷新令牌对，必须有会话存储，且不支持
// WithClaims。签名与校验委托给 components/token；业务权限归调用方。
//
// # Features | 功能
//
// - JwtAuth with pluggable claims and an optional revocation store | JwtAuth：可插拔 claims、可选吊销存储
// - SessionAuth with one-time refresh rotation | SessionAuth：刷新令牌一次性轮换
// - Generic net/http middleware plus Bearer/cookie/query token sources | 通用 net/http 中间件与 Bearer/Cookie/Query 令牌来源
// - StatusOf maps auth errors to HTTP statuses and challenges | StatusOf 把认证错误映射为 HTTP 状态码与质询
//
// # Example | 示例
//
//	auth, err := NewSessionAuth(store)
//	pair, err := auth.Issue(ctx, "10001")
//	claims, err := auth.ParseAccess(ctx, pair.AccessToken)
package authn

import (
	"context"

	"github.com/golang-jwt/jwt/v5"
)

// Token carries an issued token's string value, type and expiry | 携带签发令牌的值、类型与过期时间.
type Token interface {
	// GetToken returns the token string | GetToken 返回令牌字符串.
	GetToken() string
	// GetTokenType returns the token type | GetTokenType 返回令牌类型.
	GetTokenType() string
	// GetExpireAt returns the token expiration unix timestamp | GetExpireAt 返回令牌过期的 unix 时间戳.
	GetExpireAt() int64
	// EncodeToJSON encodes the token to JSON | EncodeToJSON 将令牌编码为 JSON.
	EncodeToJSON() ([]byte, error)
}

// Authenticator does token issuing, revocation and verification (including the
// revocation list) | Authenticator 只做三件事：签发、吊销、校验（含吊销名单）。
type Authenticator interface {
	// Sign generates and returns a new token | Sign 生成并返回新令牌.
	Sign(ctx context.Context) (Token, error)

	// Destroy revokes a token | Destroy 吊销令牌.
	Destroy(ctx context.Context, accessToken string) error

	// ParseClaims parses the token and returns its claims.
	// It returns the very object produced by the claims factory (not a copy),
	// so the factory must return a fresh object each call — sharing one object
	// lets concurrent requests overwrite each other. The revocation list check
	// is skipped when no store is configured.
	// ParseClaims 解析令牌并返回 claims。
	// 返回的是 claims 工厂产出的那个对象（不是副本），所以工厂必须每次返回新对象——
	// 共享一个对象时并发请求会互相覆写。没有存储时跳过吊销名单检查。
	ParseClaims(ctx context.Context, accessToken string) (jwt.Claims, error)
}
