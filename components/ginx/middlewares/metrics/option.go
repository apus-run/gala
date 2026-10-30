package metrics

import (
	"strings"
)

// Option set the metrics options.
type Option interface {
	apply(*Options)
}

// OptionFunc adapts a plain function to Option.
type OptionFunc func(*Options)

func (f OptionFunc) apply(o *Options) {
	f(o)
}

type Options struct {
	metricsPath          string
	ignoreStatusCodes    map[int]struct{}
	ignoreRequestPaths   map[string]struct{}
	ignoreRequestMethods map[string]struct{}
}

// DefaultOptions returns a new Options holding the package defaults.
func DefaultOptions() *Options {
	return &Options{
		metricsPath:          "/metrics",
		ignoreStatusCodes:    nil,
		ignoreRequestPaths:   nil,
		ignoreRequestMethods: nil,
	}
}

func (o *Options) apply(opts ...Option) {
	for _, opt := range opts {
		opt.apply(o)
	}
}

// WithMetricsPath set metrics path
func WithMetricsPath(metricsPath string) Option {
	return OptionFunc(func(o *Options) {
		o.metricsPath = metricsPath
	})
}

// WithIgnoreStatusCodes ignore status codes
func WithIgnoreStatusCodes(statusCodes ...int) Option {
	return OptionFunc(func(o *Options) {
		codeMaps := make(map[int]struct{}, len(statusCodes))
		for _, code := range statusCodes {
			codeMaps[code] = struct{}{}
		}
		o.ignoreStatusCodes = codeMaps
	})
}

// WithIgnoreRequestPaths ignore request paths
func WithIgnoreRequestPaths(paths ...string) Option {
	return OptionFunc(func(o *Options) {
		pathMaps := make(map[string]struct{}, len(paths))
		for _, path := range paths {
			pathMaps[path] = struct{}{}
		}
		o.ignoreRequestPaths = pathMaps
	})
}

// WithIgnoreRequestMethods ignore request methods
func WithIgnoreRequestMethods(methods ...string) Option {
	return OptionFunc(func(o *Options) {
		methodMaps := make(map[string]struct{}, len(methods))
		for _, method := range methods {
			methodMaps[strings.ToUpper(method)] = struct{}{}
		}
		o.ignoreRequestMethods = methodMaps
	})
}

func (o *Options) isIgnoreCodeStatus(statusCode int) bool {
	if o.ignoreStatusCodes == nil {
		return false
	}
	_, ok := o.ignoreStatusCodes[statusCode]
	return ok
}

func (o *Options) isIgnorePath(path string) bool {
	if o.ignoreRequestPaths == nil {
		return false
	}
	_, ok := o.ignoreRequestPaths[path]
	return ok
}

func (o *Options) checkIgnoreMethod(method string) bool {
	if o.ignoreRequestMethods == nil {
		return false
	}
	_, ok := o.ignoreRequestMethods[strings.ToUpper(method)]
	return ok
}
