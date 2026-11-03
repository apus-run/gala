package gateway

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/url"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// AnnotatorFunc is the annotator function is for injecting metadata from http request into gRPC context
// AnnotatorFunc 把 HTTP 请求中的元数据注入 gRPC context.
type AnnotatorFunc func(context.Context, *http.Request) metadata.MD

// HandlerFunc registers a service handler on the gateway mux | HandlerFunc 在网关 mux 上注册服务处理函数.
type HandlerFunc func(ctx context.Context, mux *runtime.ServeMux, conn *grpc.ClientConn) error

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
	// network 指定服务器监听的网络类型，如 "tcp" 或 "udp"。
	// network is the listening network type, e.g. "tcp" or "udp".
	network string
	// addr 指定服务器监听的地址。
	// addr is the listening address.
	addr string
	// lis 指定服务器的监听器。
	// lis is the server listener.
	lis net.Listener
	// tlsConf 指定 TLS 配置。
	// tlsConf is the TLS configuration.
	tlsConf *tls.Config

	// shutdownFunc 是一个在服务器关闭时调用的函数，用于执行清理操作。
	// shutdownFunc runs when the server shuts down, for cleanup.
	shutdownFunc func()

	endpoint *url.URL
	err      error

	conn                    *grpc.ClientConn
	serveMuxOpts            []runtime.ServeMuxOption
	registerServiceHandlers []HandlerFunc
	annotators              []AnnotatorFunc
}

// DefaultOptions returns a new ServerOptions holding the package defaults | DefaultOptions 返回持有包默认值的 ServerOptions.
func DefaultOptions() *ServerOptions {
	return &ServerOptions{
		network:      "tcp",
		addr:         ":0",
		shutdownFunc: func() {},
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

// WithListener sets the listener | WithListener 设置监听器.
func WithListener(lis net.Listener) ServerOption {
	return ServerOptionFunc(func(o *ServerOptions) {
		o.lis = lis
	})
}

// WithTLSConf sets the TLS configuration | WithTLSConf 设置 TLS 配置.
func WithTLSConf(tlsConf *tls.Config) ServerOption {
	return ServerOptionFunc(func(o *ServerOptions) {
		o.tlsConf = tlsConf
	})
}

// WithNetwork sets the listening network type | WithNetwork 设置监听的网络类型.
func WithNetwork(network string) ServerOption {
	return ServerOptionFunc(func(o *ServerOptions) {
		o.network = network
	})
}

// WithAddress sets the listening address | WithAddress 设置监听地址.
func WithAddress(addr string) ServerOption {
	return ServerOptionFunc(func(o *ServerOptions) {
		o.addr = addr
	})
}

// WithShutdownFunc sets the shutdown cleanup function | WithShutdownFunc 设置关闭时的清理函数.
func WithShutdownFunc(shutdownFunc func()) ServerOption {
	return ServerOptionFunc(func(o *ServerOptions) {
		if shutdownFunc == nil {
			shutdownFunc = func() {}
		}
		o.shutdownFunc = shutdownFunc
	})
}

// WithConn sets the gRPC client connection | WithConn 设置 gRPC 客户端连接.
func WithConn(conn *grpc.ClientConn) ServerOption {
	return ServerOptionFunc(func(o *ServerOptions) {
		o.conn = conn
	})
}

// WithServeMuxOpts sets extra ServeMux options | WithServeMuxOpts 设置额外的 ServeMux 选项.
func WithServeMuxOpts(opts ...runtime.ServeMuxOption) ServerOption {
	return ServerOptionFunc(func(o *ServerOptions) {
		o.serveMuxOpts = opts
	})
}

// WithRegisterServiceHandlers sets the service handlers to register | WithRegisterServiceHandlers 设置要注册的服务处理函数.
func WithRegisterServiceHandlers(handlers ...HandlerFunc) ServerOption {
	return ServerOptionFunc(func(o *ServerOptions) {
		o.registerServiceHandlers = handlers
	})
}

// WithAnnotators sets the metadata annotators | WithAnnotators 设置元数据注入器.
func WithAnnotators(annotators ...AnnotatorFunc) ServerOption {
	return ServerOptionFunc(func(o *ServerOptions) {
		o.annotators = annotators
	})
}

// CombineAnnotators combines multiple AnnotatorFunc into a single AnnotatorFunc | CombineAnnotators 把多个 AnnotatorFunc 合并为一个.
func CombineAnnotators(annotators ...AnnotatorFunc) AnnotatorFunc {
	return func(ctx context.Context, r *http.Request) metadata.MD {
		mds := make([]metadata.MD, 0, len(annotators))
		for _, annotator := range annotators {
			mds = append(mds, annotator(ctx, r))
		}
		return metadata.Join(mds...)
	}
}
