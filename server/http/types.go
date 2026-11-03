package http

import (
	"crypto/tls"
	"net"
	"net/http"
	"net/url"
	"time"
)

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

	// lis is the server listener | lis 是服务器的监听器.
	lis net.Listener

	// handler 指定 HTTP 服务器的处理器。
	// handler is the HTTP request handler.
	handler http.Handler

	// tlsConf 指定 TLS 配置。
	// tlsConf is the TLS configuration.
	tlsConf *tls.Config

	// endpoint is the resolved registry endpoint | endpoint 是解析出的注册端点.
	endpoint *url.URL
	// err holds a listener/endpoint error for later return | err 暂存监听或端点错误以便稍后返回.
	err error

	// 核心性能配置（精简版）
	// Core performance settings (trimmed).
	readTimeout    time.Duration // read timeout | 读取超时
	writeTimeout   time.Duration // write timeout | 写入超时
	idleTimeout    time.Duration // idle timeout | 空闲超时
	maxHeaderBytes int           // max header size | 最大请求头大小
}

// DefaultOptions returns a new ServerOptions holding the package defaults | DefaultOptions 返回持有包默认值的 ServerOptions.
func DefaultOptions() *ServerOptions {
	return &ServerOptions{
		network:        "tcp",
		addr:           ":0",
		handler:        http.DefaultServeMux,
		readTimeout:    5 * time.Second,  // 5s read timeout | 5 秒读取超时
		writeTimeout:   30 * time.Second, // 30s write timeout | 30 秒写入超时
		idleTimeout:    60 * time.Second, // 60s idle timeout | 60 秒空闲超时
		maxHeaderBytes: 1 << 20,          // 1MB header limit | 1MB 请求头限制
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

// WithListener 设置服务器的监听器。
// WithListener sets the server listener.
func WithListener(listener net.Listener) ServerOption {
	return ServerOptionFunc(func(options *ServerOptions) {
		options.lis = listener
	})
}

// WithHandler 设置 HTTP 服务器的处理器。
// WithHandler sets the HTTP request handler; nil is ignored.
func WithHandler(handler http.Handler) ServerOption {
	return ServerOptionFunc(func(options *ServerOptions) {
		if handler == nil {
			return
		}
		options.handler = handler
	})
}

// WithReadTimeout 设置读取超时时间。
// WithReadTimeout sets the read timeout.
func WithReadTimeout(timeout time.Duration) ServerOption {
	return ServerOptionFunc(func(options *ServerOptions) {
		options.readTimeout = timeout
	})
}

// WithWriteTimeout 设置写入超时时间。
// WithWriteTimeout sets the write timeout.
func WithWriteTimeout(timeout time.Duration) ServerOption {
	return ServerOptionFunc(func(options *ServerOptions) {
		options.writeTimeout = timeout
	})
}

// WithIdleTimeout 设置空闲连接超时时间。
// WithIdleTimeout sets the idle connection timeout.
func WithIdleTimeout(timeout time.Duration) ServerOption {
	return ServerOptionFunc(func(options *ServerOptions) {
		options.idleTimeout = timeout
	})
}

// WithMaxHeaderBytes 设置最大请求头大小。
// WithMaxHeaderBytes sets the maximum header size.
func WithMaxHeaderBytes(maxBytes int) ServerOption {
	return ServerOptionFunc(func(options *ServerOptions) {
		options.maxHeaderBytes = maxBytes
	})
}

// WithCompressionLevel 设置响应压缩级别。
// WithCompressionLevel sets the response compression level.
//
// Deprecated: kept for signature compatibility, no longer used | 保留函数签名，但不再使用.
func WithCompressionLevel(level int) ServerOption {
	return ServerOptionFunc(func(options *ServerOptions) {
	})
}
