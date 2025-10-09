package http

import (
	"crypto/tls"
	"net"
	"net/http"
	"net/url"
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
}

func NewServerOptions() *ServerOptions {
	return &ServerOptions{
		network: "tcp",
		addr:    ":0",
		handler: http.DefaultServeMux,
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
