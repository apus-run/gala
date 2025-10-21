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
type AnnotatorFunc func(context.Context, *http.Request) metadata.MD

type HandlerFunc func(ctx context.Context, mux *runtime.ServeMux, conn *grpc.ClientConn) error

// ServerOption 是一个函数类型，用于设置 ServerOptions 的各个字段。
type ServerOption func(*ServerOptions)

// ServerOptions 定义了服务器的配置选项。
type ServerOptions struct {
	// network 指定服务器监听的网络类型，如 "tcp" 或 "udp"。
	network string
	// addr 指定服务器监听的地址。
	addr string
	// lis 指定服务器的监听器。
	lis net.Listener
	// tlsConf 指定 TLS 配置。
	tlsConf *tls.Config

	// shutdownFunc 是一个在服务器关闭时调用的函数，用于执行清理操作。
	shutdownFunc func()

	endpoint *url.URL
	err      error

	conn                    *grpc.ClientConn
	serveMuxOpts            []runtime.ServeMuxOption
	registerServiceHandlers []HandlerFunc
	annotators              []AnnotatorFunc
}

func NewServerOptions() *ServerOptions {
	return &ServerOptions{
		network:      "tcp",
		addr:         ":0",
		shutdownFunc: func() {},
	}
}

func Apply(opts ...ServerOption) *ServerOptions {
	options := NewServerOptions()
	for _, opt := range opts {
		opt(options)
	}
	return options
}

func WithListener(lis net.Listener) ServerOption {
	return func(o *ServerOptions) {
		o.lis = lis
	}
}

func WithTLSConf(tlsConf *tls.Config) ServerOption {
	return func(o *ServerOptions) {
		o.tlsConf = tlsConf
	}
}

func WithNetwork(network string) ServerOption {
	return func(o *ServerOptions) {
		o.network = network
	}
}

func WithAddress(addr string) ServerOption {
	return func(o *ServerOptions) {
		o.addr = addr
	}
}

func WithShutdownFunc(shutdownFunc func()) ServerOption {
	return func(o *ServerOptions) {
		o.shutdownFunc = shutdownFunc
	}
}

func WithConn(conn *grpc.ClientConn) ServerOption {
	return func(o *ServerOptions) {
		o.conn = conn
	}
}

func WithServeMuxOpts(opts ...runtime.ServeMuxOption) ServerOption {
	return func(o *ServerOptions) {
		o.serveMuxOpts = opts
	}
}

func WithRegisterServiceHandlers(handlers ...HandlerFunc) ServerOption {
	return func(o *ServerOptions) {
		o.registerServiceHandlers = handlers
	}
}

func WithAnnotators(annotators ...AnnotatorFunc) ServerOption {
	return func(o *ServerOptions) {
		o.annotators = annotators
	}
}

// CombineAnnotators combines multiple AnnotatorFunc into a single AnnotatorFunc
func CombineAnnotators(annotators ...AnnotatorFunc) AnnotatorFunc {
	return func(ctx context.Context, r *http.Request) metadata.MD {
		mds := make([]metadata.MD, 0, len(annotators))
		for _, annotator := range annotators {
			mds = append(mds, annotator(ctx, r))
		}
		return metadata.Join(mds...)
	}
}
