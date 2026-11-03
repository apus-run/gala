package authn

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	rdb "github.com/apus-run/gala/components/authn/store"
	"github.com/apus-run/gala/components/token"
	"github.com/apus-run/gala/pkg/jwtx"
)

var _ Authenticator = (*JwtAuth)(nil)

// Reuse jwtx's errors directly so the two definitions cannot drift apart.
// 直接复用 jwtx 的错误，避免两处各定义一份再慢慢漂移。
var (
	ErrTokenInvalid           = jwtx.ErrTokenInvalid
	ErrUnSupportSigningMethod = jwtx.ErrUnSupportSigningMethod
	ErrSignToken              = jwtx.ErrSignToken
	ErrGetKey                 = jwtx.ErrGetKey
	ErrMissingClaims          = jwtx.ErrMissingClaims
	ErrEmptySessionStore      = errors.New("session store is empty")
	// ErrTokenRevoked wraps ErrTokenInvalid, so StatusOf and existing errors.Is
	// checks are unchanged; it only lets logs tell revocation from forgery.
	// ErrTokenRevoked 包装 ErrTokenInvalid，StatusOf 与现有 errors.Is 检查不受影响，
	// 只是让日志能区分吊销与伪造。
	ErrTokenRevoked = fmt.Errorf("%w: revoked", ErrTokenInvalid)
)

// JwtAuth implements the authn.Authenticator interface.
// Signing and verification are delegated to components/token's JwtManager;
// authn itself only owns the revocation list and the client-facing shape.
// JwtAuth 实现 authn.Authenticator 接口。
// 签发/校验交给 components/token 的 JwtManager，authn 自己只管吊销名单和客户端交付形态。
type JwtAuth struct {
	// Do not embed *Options: embedding would promote Apply onto JwtAuth and
	// let callers mutate an already-constructed instance.
	// 不嵌入 *Options：嵌入会把 Apply 提升到 JwtAuth，调用方就能改掉已构造的实例。
	options *Options
	store   rdb.Storer
	manager *token.JwtManager[struct{}]
}

// NewJwtAuth builds a JwtAuth over an optional revocation store | NewJwtAuth 基于可选的吊销存储构建 JwtAuth.
func NewJwtAuth(store rdb.Storer, opts ...Option) *JwtAuth {
	options := NewOptions(opts...)
	if options.claims == nil {
		// keep a nil-returning factory when none is configured: call sites only
		// need to check claims == nil instead of the function itself
		// 没配工厂时留一个返回 nil 的：调用点只需判 claims == nil，不必先判函数本身
		options.claims = func() jwt.Claims { return nil }
	}
	return &JwtAuth{
		options: options,
		store:   store,
		// the claims type is decided by the caller's factory; the manager's
		// type parameter is unused (only GenerateClaims/ParseClaims are called)
		// claims 类型由调用方的工厂决定，manager 的泛型参数用不到（只走 GenerateClaims/ParseClaims）
		manager: token.NewJwtManager[struct{}](options.jwtOpts...),
	}
}

// Sign issues a token from the configured claims factory | Sign 用配置的 claims 工厂签发令牌.
func (j *JwtAuth) Sign(_ context.Context) (Token, error) {
	claims := j.options.claims()
	if claims == nil {
		return nil, ErrMissingClaims
	}

	// exp is the caller's claims' responsibility. The token layer only checks
	// that exp exists, not whether it has passed, so check it here ourselves:
	// signing an already-expired token is always a caller bug and must not
	// report success.
	// exp 由调用方的 claims 负责。token 层只要求 exp 存在、不管早晚，所以这里自己判一次：
	// 签出一个已经过期的 token 一定是调用方的 bug，不该回报成功。
	exp, err := claims.GetExpirationTime()
	if err != nil || exp == nil {
		return nil, jwtx.ErrNoExpiration
	}
	if !exp.After(time.Now()) {
		return nil, fmt.Errorf("%w: claims 的 exp %s 已过期", ErrTokenInvalid, exp.Format(time.RFC3339))
	}

	tokenString, err := j.manager.GenerateClaims(claims)
	if err != nil {
		return nil, err
	}

	return &tokenInfo{
		Token:     tokenString,
		Type:      "Bearer",
		ExpiresAt: exp.Unix(),
	}, nil
}

// Destroy records the token in the revocation list. Without a store it
// returns ErrEmptySessionStore instead of silently succeeding — otherwise the
// caller believes logout worked while the token stays valid until exp.
// Destroy 把 token 记进吊销名单。没有存储时**返回 ErrEmptySessionStore**，不再静默成功——
// 否则调用方以为登出生效了，token 却会一直有效到 exp。
func (j *JwtAuth) Destroy(ctx context.Context, accessToken string) error {
	if j.store == nil {
		return ErrEmptySessionStore
	}

	claims, err := j.ParseClaims(ctx, accessToken)
	if err != nil {
		return err
	}

	exp, err := claims.GetExpirationTime()
	if err != nil || exp == nil {
		return ErrTokenInvalid
	}

	// align the revocation record's expiry with the token's own exp so it
	// disappears naturally once expired
	// 吊销记录的过期时间对齐 token 自己的 exp，过期后让它自然消失。
	return j.store.Set(ctx, accessToken, "1", time.Until(exp.Time))
}

// ParseClaims verifies the token and checks the revocation list, returning the
// claims produced by the caller's factory (type matches the factory).
// Parsing writes **into the very object returned by the factory**, so the
// factory must return a fresh object each call — with a shared object,
// concurrent requests overwrite each other's claims (A gets B's identity).
// The revocation list check is skipped when no store is configured.
// ParseClaims 校验 token 并检查吊销名单，返回调用方工厂产出的 claims（类型与工厂一致）。
// 解析是**写进工厂返回的那个对象**里的，所以工厂必须每次返回新对象——返回共享对象时，
// 并发请求会互相覆写彼此的 claims（A 拿到 B 的身份）。没有存储时跳过吊销名单检查。
func (j *JwtAuth) ParseClaims(ctx context.Context, accessToken string) (jwt.Claims, error) {
	if accessToken == "" {
		return nil, ErrTokenInvalid
	}

	claims := j.options.claims()
	if claims == nil {
		return nil, ErrMissingClaims
	}

	if err := j.manager.ParseClaims(accessToken, claims); err != nil {
		return nil, err
	}

	if j.store != nil {
		exists, err := j.store.Check(ctx, accessToken)
		if err != nil {
			return nil, err
		}
		if exists {
			return nil, ErrTokenRevoked
		}
	}

	return claims, nil
}

// GenerateToken is a shortcut when only the token string is needed (Sign also
// returns the type and expiry) | GenerateToken 只要 token 串时的快捷方式（Sign 会额外给出类型和过期时间）.
func (j *JwtAuth) GenerateToken(_ context.Context) (string, error) {
	claims := j.options.claims()
	if claims == nil {
		return "", ErrMissingClaims
	}
	return j.manager.GenerateClaims(claims)
}

// newClaims has been inlined at its call sites; the store nil-check is also
// handled in place (Destroy errors, ParseClaims skips).
// newClaims 已内联到调用点；store 的判空也各自就地处理（Destroy 报错、ParseClaims 跳过）。
