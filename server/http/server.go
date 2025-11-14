package http

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"

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

func NewServer(opts ...ServerOption) *Server {
	options := Apply(opts...)

	srv := &Server{
		opts: options,
	}

	srv.Server = &http.Server{
		Handler:        srv,
		TLSConfig:      options.tlsConf,
		ReadTimeout:    options.readTimeout,
		WriteTimeout:   options.writeTimeout,
		IdleTimeout:    options.idleTimeout,
		MaxHeaderBytes: options.maxHeaderBytes,
	}

	return srv
}

func (s *Server) Start(ctx context.Context) error {
	if err := s.listenAndEndpoint(); err != nil {
		return err
	}

	s.BaseContext = func(listener net.Listener) context.Context {
		return ctx
	}

	var err error
	if s.opts.tlsConf != nil {
		slog.Info("[HTTPS] server listen on", "address", s.opts.addr)
		err = s.ServeTLS(s.opts.lis, "", "")
	} else {
		slog.Info("[HTTP] server listen on", "address", s.opts.addr)
		err = s.Serve(s.opts.lis)
	}

	if !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) Stop(ctx context.Context) error {
	return shutdown.ShutdownWithContext(ctx, func(ctx context.Context) error {
		return s.Server.Shutdown(ctx)
	}, func() error {
		if err := s.Server.Close(); err != nil {
			return err
		}

		return nil
	})
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

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.opts.handler.ServeHTTP(w, r)
}

// Health check server is healthy.
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
		s.opts.endpoint = endpoint.NewEndpoint(endpoint.Scheme("http", s.opts.tlsConf != nil), addr)
	}
	return s.opts.err
}
