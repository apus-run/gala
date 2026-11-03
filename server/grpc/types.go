package grpc

import (
	"context"
	"crypto/tls"
	"net"
	"net/url"

	"google.golang.org/grpc"
)

// RegisterFunc registers services on a gRPC registrar | RegisterFunc 在 gRPC registrar 上注册服务.
type RegisterFunc func(grpc.ServiceRegistrar)

// ServerOption 是一个函数类型，用于设置 ServerOptions 的各个字段。
// ServerOption is a function type that sets the fields of ServerOptions.
type ServerOption interface {
	apply(*ServerOptions)
}

// ServerOptionFunc adapts a plain function to ServerOption.
// ServerOptionFunc 把普通函数适配为 ServerOption.
type ServerOptionFunc func(*ServerOptions)

// apply runs the adapted function | apply 执行被适配的函数.
func (f ServerOptionFunc) apply(o *ServerOptions) {
	f(o)
}

// ServerOptions 定义了服务器的配置选项。
// ServerOptions holds the server configuration.
type ServerOptions struct {
	// baseCtx 是服务器的基础上下文。
	// baseCtx is the base context of the server.
	baseCtx context.Context

	// Network 指定服务器监听的网络类型，如 "tcp" 或 "udp"。
	// network is the listening network type, e.g. "tcp" or "udp".
	network string
	// addr 指定服务器监听的地址。
	// addr is the listening address.
	addr string

	// lis is the server listener | lis 是服务器的监听器.
	lis net.Listener

	// grpcOpts 指定 gRPC 服务器的选项。
	// grpcOpts are the gRPC server options.
	grpcOpts []grpc.ServerOption

	// tlsConf 指定 TLS 配置。
	// tlsConf is the TLS configuration.
	tlsConf *tls.Config

	// endpoint is the resolved registry endpoint | endpoint 是解析出的注册端点.
	endpoint *url.URL
	// err holds a listener/endpoint error for later return | err 暂存监听或端点错误以便稍后返回.
	err error

	// disableReflection 指定是否禁用 gRPC 反射服务。
	// disableReflection disables the gRPC reflection service.
	disableReflection bool
}

// DefaultOptions returns a new ServerOptions holding the package defaults | DefaultOptions 返回持有包默认值的 ServerOptions.
func DefaultOptions() *ServerOptions {
	return &ServerOptions{
		baseCtx: context.Background(),
		network: "tcp",
		addr:    ":0",
	}
}

// NewServerOptions returns a new ServerOptions with the package defaults.
// NewServerOptions 返回带包默认值的 ServerOptions.
//
// Deprecated: use DefaultOptions | 已废弃，改用 DefaultOptions.
func NewServerOptions() *ServerOptions {
	return DefaultOptions()
}

// NewOptions builds ServerOptions from the defaults, then applies opts in order | NewOptions 从默认值出发按顺序应用 opts 构造 ServerOptions.
func NewOptions(opts ...ServerOption) *ServerOptions {
	o := DefaultOptions()
	o.Apply(opts...)
	return o
}

// Apply 按顺序原地应用选项，不重置默认值、不做校验。
// Apply applies the options in place, in order; it neither resets defaults nor validates.
func (o *ServerOptions) Apply(opts ...ServerOption) {
	for _, opt := range opts {
		opt.apply(o)
	}
}

// WithNetwork 设置服务器监听的网络类型。
// WithNetwork sets the listening network type.
func WithNetwork(network string) ServerOption {
	return ServerOptionFunc(func(options *ServerOptions) {
		options.network = network
	})
}

// WithAddress 设置服务器监听的地址。
// WithAddress sets the listening address.
func WithAddress(address string) ServerOption {
	return ServerOptionFunc(func(options *ServerOptions) {
		options.addr = address
	})
}

// WithTlsConfig 设置服务器的 TLS 配置。
// WithTlsConfig sets the server TLS configuration.
func WithTlsConfig(tlsConfig *tls.Config) ServerOption {
	return ServerOptionFunc(func(options *ServerOptions) {
		options.tlsConf = tlsConfig
	})
}

// WithDisableReflection disables the gRPC reflection service | WithDisableReflection 禁用 gRPC 反射服务.
func WithDisableReflection(disableReflection bool) ServerOption {
	return ServerOptionFunc(func(options *ServerOptions) {
		options.disableReflection = disableReflection
	})
}

// WithListener 设置服务器的监听器。
// WithListener sets the server listener.
func WithListener(listener net.Listener) ServerOption {
	return ServerOptionFunc(func(options *ServerOptions) {
		options.lis = listener
	})
}

// WithBaseContext 设置服务器的基础上下文。
// WithBaseContext sets the base context of the server.
func WithBaseContext(baseCtx context.Context) ServerOption {
	return ServerOptionFunc(func(options *ServerOptions) {
		options.baseCtx = baseCtx
	})
}

// WithGrpcOptions 设置 gRPC 服务器的选项。
// WithGrpcOptions appends gRPC server options.
func WithGrpcOptions(grpcOpts ...grpc.ServerOption) ServerOption {
	return ServerOptionFunc(func(options *ServerOptions) {
		options.grpcOpts = append(options.grpcOpts, grpcOpts...)
	})
}
