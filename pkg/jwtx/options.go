package jwtx

import (
	"github.com/golang-jwt/jwt/v5"
)

type ClaimsFunc func() jwt.Claims

// Option is jwt option.
type Option interface {
	apply(*Options)
}

// OptionFunc adapts a plain function to Option.
type OptionFunc func(*Options)

func (f OptionFunc) apply(o *Options) {
	f(o)
}

// Options is jwt options.
type Options struct {
	signingMethod jwt.SigningMethod
	claims        ClaimsFunc
	tokenHeader   map[string]any
	parserOptions []jwt.ParserOption
}

// DefaultOptions .
func DefaultOptions() *Options {
	return &Options{
		signingMethod: jwt.SigningMethodHS256,
	}
}

// NewOptions 从 DefaultOptions 出发，按顺序应用 opts。
func NewOptions(opts ...Option) *Options {
	o := DefaultOptions()
	o.Apply(opts...)
	return o
}

// Apply 按顺序原地应用 opts，不重置默认值、不做校验。
func (o *Options) Apply(opts ...Option) {
	for _, opt := range opts {
		opt.apply(o)
	}
}

// WithSigningMethod with signing method option.
func WithSigningMethod(method jwt.SigningMethod) Option {
	return OptionFunc(func(o *Options) {
		o.signingMethod = method
	})
}

// WithClaims with customer claim
// If you use it in Server, f needs to return a new jwt.Claims object each time to avoid concurrent write problems
// If you use it in Client, f only needs to return a single object to provide performance
// f 返回的 claims 必须自带 exp（jwtx 不签没有 exp 的 token）
func WithClaims(f ClaimsFunc) Option {
	return OptionFunc(func(o *Options) {
		o.claims = f
	})
}

// WithTokenHeader withe customer tokenHeader for client side
func WithTokenHeader(header map[string]any) Option {
	return OptionFunc(func(o *Options) {
		o.tokenHeader = header
	})
}

// WithParserOptions 透传 jwt/v5 的解析选项，例如 WithAudience/WithIssuer (多服务共用密钥时隔离
// 受众)、WithLeeway (时钟偏移)、WithoutClaimsValidation (过期的 token 也要能解析出来做刷新)。
// 签名算法的校验不受影响，仍由 WithSigningMethod 决定。
func WithParserOptions(opts ...jwt.ParserOption) Option {
	return OptionFunc(func(o *Options) {
		o.parserOptions = append(o.parserOptions, opts...)
	})
}
