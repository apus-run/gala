package authn

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/apus-run/gala/components/token"
	"github.com/apus-run/gala/pkg/option"
)

type (
	// OptionFunc adapts a plain function to Option | OptionFunc 把普通函数适配成 Option.
	OptionFunc func(*Options)

	// Option only covers authn's own switches. The JWT configuration surface
	// is not duplicated here — secret, algorithm and audience/issuer checks
	// come from components/token's options (WithJWTOptions).
	// Option 只覆盖 authn 自己的开关。JWT 的配置面不在这里重复一份——
	// 密钥、算法、audience/issuer 校验等直接用 components/token 的选项（WithJWTOptions）。
	Option interface {
		apply(*Options)
	}

	// Options is the authn configuration | authn 的配置项.
	Options struct {
		accessTTL, refreshTTL time.Duration
		claims                func() jwt.Claims
		jwtOpts               []option.Option[token.JwtConfig]
	}
)

func (f OptionFunc) apply(o *Options) {
	f(o)
}

// DefaultOptions returns a new Options holding the package defaults.
// DefaultOptions 返回带包默认值的新 Options。
func DefaultOptions() *Options {
	return &Options{accessTTL: AccessTokenTTL, refreshTTL: RefreshTokenTTL}
}

// NewOptions builds Options from the defaults, then applies opts in order.
// NewOptions 从默认值构建 Options，再按顺序应用 opts。
func NewOptions(opts ...Option) *Options {
	o := DefaultOptions()
	o.Apply(opts...)
	return o
}

// Validate checks the session lifetimes. It does not change the Options.
// JwtAuth ignores these values, so only NewSessionAuth calls it.
// Validate 校验会话存活时间，不会修改 Options。
// JwtAuth 不使用这些值，所以只有 NewSessionAuth 调用它。
func (o *Options) Validate() error {
	if o.accessTTL < time.Second || o.refreshTTL < o.accessTTL {
		return errors.New("session TTLs must satisfy 1s <= access <= refresh")
	}
	return nil
}

// Apply applies opts in order, without resetting defaults and without validating.
// Apply 按顺序应用选项，不重置默认值、不做校验。
func (o *Options) Apply(opts ...Option) {
	for _, opt := range opts {
		opt.apply(o)
	}
}

// WithClaims sets the claims factory: issuing uses it to produce claims and
// verification uses it as the parse target, so the type must agree. The server
// must return a fresh object each call to avoid concurrent writes (see the
// notes on token.WithClaims).
// WithClaims 设置 claims 工厂：签发用它产出 claims，校验用它作为解析目标，所以类型要一致。
// 服务端每次要返回新对象，避免并发写（见 token.WithClaims 的说明）。
//
// exp is the factory's responsibility — it is a mandatory claim on issuing,
// and without it the token layer returns jwtx.ErrNoExpiration. If the factory
// fills iss/aud, configure the same values via token.WithIssuer/WithAudience:
// verification checks them against the config (token's default issuer is
// gala/token), and a mismatch yields ErrTokenInvalidIssuer. token.WithExpiry
// only affects token's built-in claims, not the factory-produced claims.
// This option is for the legacy JwtAuth only; NewSessionAuth returns
// ErrSessionClaimsUnsupported.
// exp 由工厂负责填——这是签发必带的声明，缺了 token 层会返回 jwtx.ErrNoExpiration。
// 工厂里填了 iss/aud 的话，还要用 token.WithIssuer/WithAudience 配同一个值：校验侧按
// 配置查（token 的默认 issuer 是 gala/token），两边对不上会得到 ErrTokenInvalidIssuer。
// token.WithExpiry 只作用于 token 自己的内置 claims，对工厂产出的 claims 不生效。
// 该选项只用于旧 JwtAuth；NewSessionAuth 会返回 ErrSessionClaimsUnsupported。
func WithClaims(f func() jwt.Claims) Option {
	return OptionFunc(func(o *Options) {
		o.claims = f
	})
}

// WithJWTOptions passes JWT configuration through to components/token:
// WithSecretKey/WithAlgorithm decide how tokens are signed, and
// WithAudience/WithIssuer/WithParserOptions decide how they are verified.
// Nothing can be signed without a secret (there is no default key).
// WithJWTOptions 透传 components/token 的 JWT 配置：WithSecretKey/WithAlgorithm 决定怎么签，
// WithAudience/WithIssuer/WithParserOptions 决定怎么验。不配密钥就签不出来（没有默认密钥）。
func WithJWTOptions(opts ...option.Option[token.JwtConfig]) Option {
	return OptionFunc(func(o *Options) {
		o.jwtOpts = append(o.jwtOpts, opts...)
	})
}

// WithSessionTTL configures pair lifetimes; NewSessionAuth validates the values.
// It does not affect the legacy JwtAuth or components/token configuration.
// WithSessionTTL 配置令牌对的存活时间；NewSessionAuth 会校验这些值。
// 不影响旧版 JwtAuth 和 components/token 的配置。
func WithSessionTTL(access, refresh time.Duration) Option {
	return OptionFunc(func(o *Options) { o.accessTTL, o.refreshTTL = access, refresh })
}
