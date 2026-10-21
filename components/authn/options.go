package authn

import (
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/apus-run/gala/components/token"
	"github.com/apus-run/gala/pkg/option"
)

// Option 只覆盖 authn 自己的开关。JWT 的配置面不在这里重复一份 ——
// 密钥、算法、audience/issuer 校验等直接用 components/token 的选项（WithJWTOptions）。
type Option func(*Options)

// Options is authn options.
type Options struct {
	accessTTL, refreshTTL time.Duration
	claims                func() jwt.Claims
	jwtOpts               []option.Option[token.JwtConfig]
}

// DefaultOptions .
func DefaultOptions() *Options {
	return &Options{accessTTL: AccessTokenTTL, refreshTTL: RefreshTokenTTL}
}

func Apply(opts ...Option) *Options {
	options := DefaultOptions()
	for _, opt := range opts {
		opt(options)
	}
	return options
}

// WithClaims 设置 claims 工厂：签发用它产出 claims，校验用它作为解析目标，所以类型要一致。
// 服务端每次要返回新对象，避免并发写（见 token.WithClaims 的说明）。
//
// exp 由工厂负责填 —— 这是签发必带的声明，缺了 token 层会返回 jwtx.ErrNoExpiration。
// 工厂里填了 iss/aud 的话，还要用 token.WithIssuer/WithAudience 配同一个值：校验侧按
// 配置查（token 的默认 issuer 是 gala/token），两边对不上会得到 ErrTokenInvalidIssuer。
// token.WithExpiry 只作用于 token 自己的内置 claims，对工厂产出的 claims 不生效。
func WithClaims(f func() jwt.Claims) Option {
	return func(o *Options) {
		o.claims = f
	}
}

// WithJWTOptions 透传 components/token 的 JWT 配置：WithSecretKey/WithAlgorithm 决定怎么签，
// WithAudience/WithIssuer/WithParserOptions 决定怎么验。不配密钥就签不出来（没有默认密钥）。
func WithJWTOptions(opts ...option.Option[token.JwtConfig]) Option {
	return func(o *Options) {
		o.jwtOpts = append(o.jwtOpts, opts...)
	}
}

// WithSessionTTL configures pair lifetimes; NewSessionAuth validates the values.
// It does not affect the legacy JwtAuth or components/token configuration.
func WithSessionTTL(access, refresh time.Duration) Option {
	return func(o *Options) { o.accessTTL, o.refreshTTL = access, refresh }
}
