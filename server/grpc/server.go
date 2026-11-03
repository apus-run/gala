// Package grpc provides the gRPC transport server of the server tree |
// grpc 提供 server 体系下的 gRPC 传输服务.
//
// # Notes | 说明
//
// NewServer registers the reflection service unless
// WithDisableReflection(true) is set. Register services on the embedded
// *grpc.Server directly, or pass grpc.ServerOption via WithGrpcOptions.
// [Server] implements [server.Server] and [server.Endpointer]; Stop shuts
// down gracefully and falls back to a forced close when the context expires.
// NewServer 默认注册反射服务，除非设置 WithDisableReflection(true)。服务可
// 直接在内嵌的 *grpc.Server 上注册，或经 WithGrpcOptions 传入
// grpc.ServerOption。[Server] 实现 [server.Server] 与 [server.Endpointer]；
// Stop 先优雅停止，context 超时后强制关闭。
//
// # Example | 示例
//
//	srv := grpc.NewServer(grpc.WithAddress(":9000"))
//	pb.RegisterEchoServiceServer(srv.Server, svc)
//	_ = srv.Start(ctx)
package grpc

import (
	"context"
	"log/slog"
	"net"
	"net/url"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/reflection"

	"github.com/apus-run/gala/server"
	"github.com/apus-run/gala/server/internal/endpoint"
	"github.com/apus-run/gala/server/internal/host"
	"github.com/apus-run/gala/server/internal/shutdown"
)

var _ server.Server = (*Server)(nil)
var _ server.Endpointer = (*Server)(nil)

// Server is the gRPC transport server | Server 是 gRPC 传输服务.
type Server struct {
	*grpc.Server

	opts *ServerOptions
}

// NewServer builds the gRPC server from options, registering reflection unless disabled.
// NewServer 从选项构造 gRPC 服务，未禁用反射时注册反射服务.
func NewServer(opts ...ServerOption) *Server {
	options := NewOptions(opts...)

	srv := &Server{
		opts: options,
	}

	var grpcOpts []grpc.ServerOption

	if options.tlsConf != nil {
		grpcOpts = append(grpcOpts, grpc.Creds(credentials.NewTLS(options.tlsConf)))
	}

	if len(options.grpcOpts) > 0 {
		grpcOpts = append(grpcOpts, options.grpcOpts...)
	}

	srv.Server = grpc.NewServer(grpcOpts...)

	if !options.disableReflection {
		// reflection register
		// 注册反射服务
		reflection.Register(srv.Server)
	}

	return srv
}

// Start listens and serves gRPC | Start 监听并启动 gRPC 服务.
func (s *Server) Start(ctx context.Context) error {
	if err := s.listenAndEndpoint(); err != nil {
		return err
	}
	s.opts.baseCtx = ctx

	slog.Info("[gRPC] server listen on", "address", s.opts.addr)

	return s.Serve(s.opts.lis)
}

// Stop stops the gRPC server: graceful first, forced close as the fallback | Stop 停止 gRPC 服务：先优雅停止，超时则强制关闭.
func (s *Server) Stop(ctx context.Context) error {
	return shutdown.ShutdownWithContext(ctx, func(_ context.Context) error {
		s.Server.GracefulStop()
		return nil
	}, func() error {
		s.Server.Stop()

		return nil
	})
}

// Health reports whether the listener accepts connections | Health 报告监听器是否可接受连接.
func (s *Server) Health() bool {
	if s.opts.lis == nil {
		return false
	}
	conn, err := s.opts.lis.Accept()
	if err != nil {
		return false
	}
	e := conn.Close()
	return e == nil
}

// Endpoint return a real address to registry endpoint.
// examples:
//
//	grpc://127.0.0.1:9000?isSecure=false
//
// Endpoint 返回用于注册的真实地址。示例：
//
//	grpc://127.0.0.1:9000?isSecure=false
func (s *Server) Endpoint() (*url.URL, error) {
	if err := s.listenAndEndpoint(); err != nil {
		return nil, s.opts.err
	}
	return s.opts.endpoint, nil
}

func (s *Server) listenAndEndpoint() error {
	if s.opts.lis == nil {
		lis, err := net.Listen(s.opts.network, s.opts.addr)
		if err != nil {
			s.opts.err = err
			return err
		}
		s.opts.lis = lis
	}
	if s.opts.endpoint == nil {
		addr, err := host.Extract(s.opts.addr, s.opts.lis)
		if err != nil {
			s.opts.err = err
			return err
		}
		s.opts.endpoint = endpoint.NewEndpoint(endpoint.Scheme("grpc", s.opts.tlsConf != nil), addr)
	}
	return s.opts.err
}
