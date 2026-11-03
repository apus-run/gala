// Package gateway serves HTTP/JSON in front of gRPC services via
// grpc-gateway | gateway 基于 grpc-gateway 在 gRPC 服务之前提供 HTTP/JSON 入口.
//
// # Notes | 说明
//
// NewServer requires at least one registered service handler and usually a
// shared *grpc.ClientConn (WithConn). Protobuf enums marshal as numbers;
// TLS is on whenever WithTLSConf is set. Annotators copy HTTP request data
// into the gRPC metadata; [ParalusGatewayAnnotator] is a ready-made one.
// [Server] implements [server.Server] and [server.Endpointer].
// NewServer 要求至少注册一个服务处理函数，通常还需共享的 *grpc.ClientConn
// （WithConn）。protobuf 枚举序列化为数字；配置 WithTLSConf 即启用 TLS。
// Annotator 把 HTTP 请求数据复制进 gRPC metadata；[ParalusGatewayAnnotator]
// 是现成实现。[Server] 实现 [server.Server] 与 [server.Endpointer]。
//
// # Example | 示例
//
//	gw, err := gateway.NewServer(ctx,
//		gateway.WithAddress(":8000"),
//		gateway.WithConn(conn),
//		gateway.WithRegisterServiceHandlers(pb.RegisterEchoServiceHandler),
//	)
package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"

	"github.com/apus-run/gala/server"
	"github.com/apus-run/gala/server/internal/endpoint"
	"github.com/apus-run/gala/server/internal/host"
	"github.com/apus-run/gala/server/internal/shutdown"
)

var _ server.Server = (*Server)(nil)
var _ server.Endpointer = (*Server)(nil)

// Server is the HTTP gateway server | Server 是 HTTP 网关服务.
type Server struct {
	*http.Server

	opts *ServerOptions
}

// NewServer builds the gateway: it wires the mux, registers the service handlers,
// and requires at least one handler.
// NewServer 构造网关：组装 mux、注册服务处理函数，至少需要一个处理函数.
func NewServer(ctx context.Context, opts ...ServerOption) (*Server, error) {
	options := NewOptions(opts...)

	srv := &Server{
		opts: options,
	}

	serveMuxOpts := []runtime.ServeMuxOption{
		runtime.WithMarshalerOption(runtime.MIMEWildcard, &runtime.JSONPb{
			// 设置序列化 protobuf 数据时，枚举类型的字段以数字格式输出.
			// 否则，默认会以字符串格式输出，跟枚举类型定义不一致，带来理解成本.
			UseEnumNumbers: true,
		}), runtime.WithErrorHandler(runtime.DefaultHTTPErrorHandler),
	}

	// init annotators
	// 初始化元数据注入器
	for _, annotator := range options.annotators {
		serveMuxOpts = append(serveMuxOpts, runtime.WithMetadata(annotator))
	}

	if len(options.serveMuxOpts) > 0 {
		serveMuxOpts = append(serveMuxOpts, options.serveMuxOpts...)
	}

	// init gateway mux
	// 初始化网关 mux
	gwmux := runtime.NewServeMux(serveMuxOpts...)

	if len(options.registerServiceHandlers) == 0 {
		return nil, errors.New("at least one handler required")
	}
	for _, registerHandler := range options.registerServiceHandlers {
		if err := registerHandler(ctx, gwmux, options.conn); err != nil {
			return nil, fmt.Errorf("handler registration failed: %w", err)
		}
	}

	srv.Server = &http.Server{
		Handler:   gwmux,
		TLSConfig: options.tlsConf,
	}

	return srv, nil
}

// Start start the HTTP server | Start 启动 HTTP 服务.
func (g *Server) Start(ctx context.Context) error {
	if err := g.listenAndEndpoint(); err != nil {
		return err
	}

	g.Server.RegisterOnShutdown(g.opts.shutdownFunc)

	g.BaseContext = func(listener net.Listener) context.Context {
		return ctx
	}

	var err error
	if g.opts.tlsConf != nil {
		slog.Info("[HTTPS] server listen on", slog.String("address", g.opts.addr))
		err = g.ServeTLS(g.opts.lis, "", "")
	} else {
		slog.Info("[HTTP] server listen on", slog.String("address", g.opts.addr))
		err = g.Serve(g.opts.lis)
	}
	if !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Stop stop the HTTP server | Stop 停止 HTTP 服务.
func (g *Server) Stop(ctx context.Context) error {
	return shutdown.ShutdownWithContext(ctx,
		func(ctx context.Context) error { return g.Server.Shutdown(ctx) },
		func() error { return g.Server.Close() },
	)
}

// Endpoint return a real address to registry endpoint.
// examples:
//
//	https://127.0.0.1:8000
//	Legacy: http://127.0.0.1:8000?isSecure=false
//
// Endpoint 返回用于注册的真实地址。
// 示例：
//
//	https://127.0.0.1:8000
//	旧格式: http://127.0.0.1:8000?isSecure=false
func (s *Server) Endpoint() (*url.URL, error) {
	if err := s.listenAndEndpoint(); err != nil {
		return nil, s.opts.err
	}
	return s.opts.endpoint, nil
}

// Health reports whether the listener accepts connections | Health 报告监听器是否可接受连接.
func (s *Server) Health() bool {
	if s.opts.lis == nil || s.opts.lis.Addr() == nil {
		return false
	}
	conn, err := s.opts.lis.Accept()
	if err != nil {
		return false
	}

	e := conn.Close()
	return e == nil
}

func (g *Server) listenAndEndpoint() error {
	if g.opts.lis == nil {
		lis, err := net.Listen(g.opts.network, g.opts.addr)
		if err != nil {
			g.opts.err = err
			return err
		}
		g.opts.lis = lis
	}
	if g.opts.endpoint == nil {
		addr, err := host.Extract(g.opts.addr, g.opts.lis)
		if err != nil {
			g.opts.err = err
			return err
		}
		g.opts.endpoint = endpoint.NewEndpoint(endpoint.Scheme("http", g.opts.tlsConf != nil), addr)
	}
	return g.opts.err
}
