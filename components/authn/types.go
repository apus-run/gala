// Package authn 提供基于 JWT 的认证功能.
// authentication 对认证的一个封装.
package authn

import (
	"context"

	"github.com/golang-jwt/jwt/v5"
)

type Token interface {
	// GetToken Get token string.
	GetToken() string
	// GetTokenType Get token type.
	GetTokenType() string
	// GetExpireAt Get token expiration timestamp.
	GetExpireAt() int64
	// EncodeToJSON JSON encoding
	EncodeToJSON() ([]byte, error)
}

// Authenticator defines methods used for token processing.
// 只有三件事：签发、吊销、校验（含吊销名单）。
type Authenticator interface {
	// Sign is used to generate a token.
	Sign(ctx context.Context) (Token, error)

	// Destroy is used to destroy a token.
	Destroy(ctx context.Context, accessToken string) error

	// ParseClaims parse the token and return the claims.
	// 返回的是 claims 工厂产出的那个对象（不是副本），所以工厂必须每次返回新对象 ——
	// 共享一个对象时并发请求会互相覆写。没有存储时跳过吊销名单检查。
	ParseClaims(ctx context.Context, accessToken string) (jwt.Claims, error)
}
