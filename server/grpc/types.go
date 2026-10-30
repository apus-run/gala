package grpc

import (
	"context"
	"crypto/tls"
	"net"
	"net/url"

	"google.golang.org/grpc"
)

type RegisterFunc func(grpc.ServiceRegistrar)

// ServerOption 是一个函数类型，用于设置 ServerOptions 的各个字段。
type ServerOption interface {
	apply(*ServerOptions)
}

// ServerOptionFunc adapts a plain function to ServerOption.
type ServerOptionFunc func(*ServerOptions)

func (f ServerOptionFunc) apply(o *ServerOptions) {
	f(o)
}

// ServerOptions 定义了服务器的配置选项。
type ServerOptions struct {
	// baseCtx 是服务器的基础上下文。
	baseCtx context.Context

	// Network 指定服务器监听的网络类型，如 "tcp" 或 "udp"。
	network string
	// addr 指定服务器监听的地址。
	addr string

	lis net.Listener

	// grpcOpts 指定 gRPC 服务器的选项。
	grpcOpts []grpc.ServerOption

	// tlsConf 指定 TLS 配置。
	tlsConf *tls.Config

	endpoint *url.URL
	err      error

	// disableReflection 指定是否禁用 gRPC 反射服务。
	disableReflection bool
}

// DefaultOptions returns a new ServerOptions holding the package defaults.
func DefaultOptions() *ServerOptions {
	return &ServerOptions{
		baseCtx: context.Background(),
		network: "tcp",
		addr:    ":0",
	}
}

// NewServerOptions returns a new ServerOptions with the package defaults.
//
// Deprecated: use DefaultOptions.
func NewServerOptions() *ServerOptions {
	return DefaultOptions()
}

// NewOptions builds ServerOptions from the defaults, then applies opts in order.
func NewOptions(opts ...ServerOption) *ServerOptions {
	o := DefaultOptions()
	o.Apply(opts...)
	return o
}

// Apply 按顺序原地应用选项，不重置默认值、不做校验。
func (o *ServerOptions) Apply(opts ...ServerOption) {
	for _, opt := range opts {
		opt.apply(o)
	}
}

// WithNetwork 设置服务器监听的网络类型。
func WithNetwork(network string) ServerOption {
	return ServerOptionFunc(func(options *ServerOptions) {
		options.network = network
	})
}

// WithAddress 设置服务器监听的地址。
func WithAddress(address string) ServerOption {
	return ServerOptionFunc(func(options *ServerOptions) {
		options.addr = address
	})
}

// WithTlsConfig 设置服务器的 TLS 配置。
func WithTlsConfig(tlsConfig *tls.Config) ServerOption {
	return ServerOptionFunc(func(options *ServerOptions) {
		options.tlsConf = tlsConfig
	})
}

func WithDisableReflection(disableReflection bool) ServerOption {
	return ServerOptionFunc(func(options *ServerOptions) {
		options.disableReflection = disableReflection
	})
}

// WithListener 设置服务器的监听器。
func WithListener(listener net.Listener) ServerOption {
	return ServerOptionFunc(func(options *ServerOptions) {
		options.lis = listener
	})
}

// WithBaseContext 设置服务器的基础上下文。
func WithBaseContext(baseCtx context.Context) ServerOption {
	return ServerOptionFunc(func(options *ServerOptions) {
		options.baseCtx = baseCtx
	})
}

// WithGrpcOptions 设置 gRPC 服务器的选项。
func WithGrpcOptions(grpcOpts ...grpc.ServerOption) ServerOption {
	return ServerOptionFunc(func(options *ServerOptions) {
		options.grpcOpts = append(options.grpcOpts, grpcOpts...)
	})
}
