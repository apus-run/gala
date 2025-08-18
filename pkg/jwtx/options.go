package jwtx

import (
	"github.com/golang-jwt/jwt/v5"
)

// Option is jwt option.
type Option func(*Options)

// Options is jwt options.
type Options struct {
	signingMethod jwt.SigningMethod
	claims        func() jwt.Claims
	tokenHeader   map[string]any
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
func WithClaims(f func() jwt.Claims) Option {
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
