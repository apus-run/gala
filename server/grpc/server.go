package grpc

import (
	"context"
	"log/slog"
	"net"
	"net/url"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/apus-run/gala/server"
	"github.com/apus-run/gala/server/internal/endpoint"
	"github.com/apus-run/gala/server/internal/host"
	"github.com/apus-run/gala/server/internal/shutdown"
)

var _ server.Server = (*Server)(nil)
var _ server.Endpointer = (*Server)(nil)

type Server struct {
	*grpc.Server

	opts *ServerOptions
}

func NewServer(opts ...ServerOption) *Server {
	options := Apply(opts...)

	srv := &Server{
		opts: options,
	}

	grpcOpts := []grpc.ServerOption{}

	if options.tlsConf != nil {
		grpcOpts = append(grpcOpts, grpc.Creds(credentials.NewTLS(options.tlsConf)))
	}

	if len(options.grpcOpts) > 0 {
		grpcOpts = append(grpcOpts, options.grpcOpts...)
	}

	srv.Server = grpc.NewServer(grpcOpts...)

	return srv
}

func (s *Server) Start(ctx context.Context) error {
	if err := s.listenAndEndpoint(); err != nil {
		return s.opts.err
	}
	s.opts.baseCtx = ctx

	slog.Info("[gRPC] server listen on", "address", s.opts.addr)

	return s.Serve(s.opts.lis)
}

func (s *Server) Stop(ctx context.Context) error {
	return shutdown.ShutdownWithContext(ctx, func(_ context.Context) error {
		s.Server.GracefulStop()
		return nil
	}, func() error {
		s.Server.Stop()

		return nil
	})
}
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
