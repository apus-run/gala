package jwtx

import (
	"github.com/golang-jwt/jwt/v5"
)

type ClaimsFunc func() jwt.Claims

// Option is jwt option.
type Option func(*Options)

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

func Apply(opts ...Option) *Options {
	options := DefaultOptions()
	for _, opt := range opts {
		opt(options)
	}
	return options
}

// WithSigningMethod with signing method option.
func WithSigningMethod(method jwt.SigningMethod) Option {
	return func(o *Options) {
		o.signingMethod = method
	}
}

// WithClaims with customer claim
// If you use it in Server, f needs to return a new jwt.Claims object each time to avoid concurrent write problems
// If you use it in Client, f only needs to return a single object to provide performance
// f 返回的 claims 必须自带 exp（jwtx 不签没有 exp 的 token）
func WithClaims(f ClaimsFunc) Option {
	return func(o *Options) {
		o.claims = f
	}
}

// WithTokenHeader withe customer tokenHeader for client side
func WithTokenHeader(header map[string]any) Option {
	return func(o *Options) {
		o.tokenHeader = header
	}
}

// WithParserOptions 透传 jwt/v5 的解析选项，例如 WithAudience/WithIssuer (多服务共用密钥时隔离
// 受众)、WithLeeway (时钟偏移)、WithoutClaimsValidation (过期的 token 也要能解析出来做刷新)。
// 签名算法的校验不受影响，仍由 WithSigningMethod 决定。
func WithParserOptions(opts ...jwt.ParserOption) Option {
	return func(o *Options) {
		o.parserOptions = append(o.parserOptions, opts...)
	}
}
