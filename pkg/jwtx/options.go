package jwtx

import (
	"github.com/golang-jwt/jwt/v5"
)

// ClaimsFunc returns a fresh jwt.Claims for signing or parsing
// ClaimsFunc 返回用于签发或解析的 jwt.Claims.
type ClaimsFunc func() jwt.Claims

// Option is jwt option | Option 是 jwtx 的配置选项.
type Option interface {
	apply(*Options)
}

// OptionFunc adapts a plain function to Option | OptionFunc 把普通函数适配为 Option.
type OptionFunc func(*Options)

func (f OptionFunc) apply(o *Options) {
	f(o)
}

// Options is jwt options | Options 是 jwtx 的配置集合.
type Options struct {
	signingMethod jwt.SigningMethod
	claims        ClaimsFunc
	tokenHeader   map[string]any
	parserOptions []jwt.ParserOption
}

// DefaultOptions returns the default options: HS256 signing
// DefaultOptions 返回默认选项：HS256 签名算法.
func DefaultOptions() *Options {
	return &Options{
		signingMethod: jwt.SigningMethodHS256,
	}
}

// NewOptions starts from DefaultOptions and applies opts in order
// NewOptions 从 DefaultOptions 出发，按顺序应用 opts。
func NewOptions(opts ...Option) *Options {
	o := DefaultOptions()
	o.Apply(opts...)
	return o
}

// Apply applies opts in place, in order, without resetting defaults or validating
// Apply 按顺序原地应用 opts，不重置默认值、不做校验。
func (o *Options) Apply(opts ...Option) {
	for _, opt := range opts {
		opt.apply(o)
	}
}

// WithSigningMethod sets the signing method | WithSigningMethod 设置签名算法.
func WithSigningMethod(method jwt.SigningMethod) Option {
	return OptionFunc(func(o *Options) {
		o.signingMethod = method
	})
}

// WithClaims sets a custom claims factory.
// In a server, f must return a new jwt.Claims object each time to avoid
// concurrent write problems; in a client, returning a single object is fine
// and faster. The claims returned by f must carry their own exp, because
// jwtx refuses to sign tokens without exp.
// WithClaims 设置自定义 claims 工厂。
// 服务端使用时 f 每次都要返回新的 jwt.Claims 对象，避免并发写问题；
// 客户端使用时返回单个对象即可，性能更好。
// f 返回的 claims 必须自带 exp（jwtx 不签没有 exp 的 token）。
func WithClaims(f ClaimsFunc) Option {
	return OptionFunc(func(o *Options) {
		o.claims = f
	})
}

// WithTokenHeader sets a custom token header, mostly for the client side
// WithTokenHeader 设置自定义 token 头，多用于客户端.
func WithTokenHeader(header map[string]any) Option {
	return OptionFunc(func(o *Options) {
		o.tokenHeader = header
	})
}

// WithParserOptions passes through jwt/v5 parser options, e.g.
// WithAudience/WithIssuer (isolate audiences when services share a key),
// WithLeeway (clock skew), WithoutClaimsValidation (parse expired tokens
// for refresh). Signature algorithm checking is unaffected and stays
// governed by WithSigningMethod.
// WithParserOptions 透传 jwt/v5 的解析选项，例如 WithAudience/WithIssuer（多服务共用密钥时
// 隔离受众）、WithLeeway（时钟偏移）、WithoutClaimsValidation（过期的 token 也要能解析出来做刷新）。
// 签名算法的校验不受影响，仍由 WithSigningMethod 决定。
func WithParserOptions(opts ...jwt.ParserOption) Option {
	return OptionFunc(func(o *Options) {
		o.parserOptions = append(o.parserOptions, opts...)
	})
}
