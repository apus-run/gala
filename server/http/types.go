package http

import (
	"crypto/tls"
	"net"
	"net/http"
	"net/url"
	"time"
)

// ServerOption 是一个函数类型，用于设置 ServerOptions 的各个字段。
type ServerOption func(*ServerOptions)

// ServerOptions 定义了服务器的配置选项。
type ServerOptions struct {
	// network 指定服务器监听的网络类型，如 "tcp" 或 "udp"。
	network string
	// addr 指定服务器监听的地址。
	addr string

	lis net.Listener

	// handler 指定 HTTP 服务器的处理器。
	handler http.Handler

	// tlsConf 指定 TLS 配置。
	tlsConf *tls.Config

	endpoint *url.URL
	err      error

	// 核心性能配置（精简版）
	readTimeout    time.Duration // 读取超时
	writeTimeout   time.Duration // 写入超时
	idleTimeout    time.Duration // 空闲超时
	maxHeaderBytes int           // 最大请求头大小
}

func NewServerOptions() *ServerOptions {
	return &ServerOptions{
		network:        "tcp",
		addr:           ":0",
		handler:        http.DefaultServeMux,
		readTimeout:    5 * time.Second,  // 5秒读取超时
		writeTimeout:   30 * time.Second, // 30秒写入超时
		idleTimeout:    60 * time.Second, // 60秒空闲超时
		maxHeaderBytes: 1 << 20,          // 1MB请求头限制
	}
}

func Apply(opts ...ServerOption) *ServerOptions {
	options := NewServerOptions()
	for _, opt := range opts {
		opt(options)
	}
	return options
}

// WithNetwork 设置服务器监听的网络类型。
func WithNetwork(network string) ServerOption {
	return func(options *ServerOptions) {
		options.network = network
	}
}

// WithAddress 设置服务器监听的地址。
func WithAddress(address string) ServerOption {
	return func(options *ServerOptions) {
		options.addr = address
	}
}

// WithTlsConfig 设置服务器的 TLS 配置。
func WithTlsConfig(tlsConfig *tls.Config) ServerOption {
	return func(options *ServerOptions) {
		options.tlsConf = tlsConfig
	}
}

// WithListener 设置服务器的监听器。
func WithListener(listener net.Listener) ServerOption {
	return func(options *ServerOptions) {
		options.lis = listener
	}
}

// WithHandler 设置 HTTP 服务器的处理器。
func WithHandler(handler http.Handler) ServerOption {
	return func(options *ServerOptions) {
		options.handler = handler
	}
}

// WithReadTimeout 设置读取超时时间。
func WithReadTimeout(timeout time.Duration) ServerOption {
	return func(options *ServerOptions) {
		options.readTimeout = timeout
	}
}

// WithWriteTimeout 设置写入超时时间。
func WithWriteTimeout(timeout time.Duration) ServerOption {
	return func(options *ServerOptions) {
		options.writeTimeout = timeout
	}
}

// WithIdleTimeout 设置空闲连接超时时间。
func WithIdleTimeout(timeout time.Duration) ServerOption {
	return func(options *ServerOptions) {
		options.idleTimeout = timeout
	}
}

// WithMaxHeaderBytes 设置最大请求头大小。
func WithMaxHeaderBytes(maxBytes int) ServerOption {
	return func(options *ServerOptions) {
		options.maxHeaderBytes = maxBytes
	}
}

// WithCompressionLevel 设置响应压缩级别。
func WithCompressionLevel(level int) ServerOption {
	return func(options *ServerOptions) {
		// 保留函数签名，但不再使用
	}
}
