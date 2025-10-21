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
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/apus-run/gala/server"
	"github.com/apus-run/gala/server/internal/endpoint"
	"github.com/apus-run/gala/server/internal/host"
	"github.com/apus-run/gala/server/internal/shutdown"
)

var _ server.Server = (*Server)(nil)
var _ server.Endpointer = (*Server)(nil)

type Server struct {
	*http.Server

	opts *ServerOptions
}

func NewServer(ctx context.Context, opts ...ServerOption) (*Server, error) {
	options := Apply(opts...)

	srv := &Server{
		opts: options,
	}

	serveMuxOpts := []runtime.ServeMuxOption{
		runtime.WithMarshalerOption(runtime.MIMEWildcard, &runtime.JSONPb{
			MarshalOptions: protojson.MarshalOptions{
				// 设置序列化 protobuf 数据时，枚举类型的字段以数字格式输出.
				// 否则，默认会以字符串格式输出，跟枚举类型定义不一致，带来理解成本.
				UseEnumNumbers: true,
			},
		}), runtime.WithErrorHandler(runtime.DefaultHTTPErrorHandler),
	}

	// init annotators
	for _, annotator := range options.annotators {
		serveMuxOpts = append(serveMuxOpts, runtime.WithMetadata(annotator))
	}

	if len(options.serveMuxOpts) > 0 {
		serveMuxOpts = append(serveMuxOpts, options.serveMuxOpts...)
	}

	// init gateway mux
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

// Start start the HTTP server.
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

// Stop stop the HTTP server.
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
func (s *Server) Endpoint() (*url.URL, error) {
	if err := s.listenAndEndpoint(); err != nil {
		return nil, s.opts.err
	}
	return s.opts.endpoint, nil
}

// Health
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
