// Package http provides the plain HTTP transport server of the server tree |
// http 提供 server 体系下的原生 HTTP 传输服务.
//
// # Notes | 说明
//
// NewServer wires the handler (default http.DefaultServeMux) plus the
// read/write/idle timeouts and header-size cap from options; TLS is on
// whenever WithTlsConfig is set. [Server] implements [server.Server] and
// [server.Endpointer]; Stop shuts down gracefully and falls back to a
// forced close when the context expires.
// NewServer 组装处理器（默认 http.DefaultServeMux）与选项中的读/写/空闲超时
// 及请求头上限；配置 WithTlsConfig 即启用 TLS。[Server] 实现 [server.Server]
// 与 [server.Endpointer]；Stop 先优雅关闭，context 超时后强制关闭。
//
// # Example | 示例
//
//	srv := http.NewServer(
//		http.WithAddress(":8000"),
//		http.WithHandler(mux),
//	)
//	_ = srv.Start(ctx)
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

// Server is the HTTP transport server | Server 是 HTTP 传输服务.
type Server struct {
	*http.Server

	opts *ServerOptions
}

// NewServer builds the HTTP server from options | NewServer 从选项构造 HTTP 服务.
func NewServer(opts ...ServerOption) *Server {
	options := NewOptions(opts...)

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

// Start listens and serves HTTP or HTTPS | Start 监听并启动 HTTP/HTTPS 服务.
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

// Stop stops the HTTP server: graceful first, forced close as the fallback | Stop 停止 HTTP 服务：先优雅关闭，超时则强制关闭.
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
//
// Endpoint 返回用于注册的真实地址。示例：
//
//	https://127.0.0.1:8000
//	旧格式: http://127.0.0.1:8000?isSecure=false
func (s *Server) Endpoint() (*url.URL, error) {
	if err := s.listenAndEndpoint(); err != nil {
		return nil, s.opts.err
	}
	return s.opts.endpoint, nil
}

// ServeHTTP delegates to the configured handler | ServeHTTP 委托给配置的处理器.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.opts.handler.ServeHTTP(w, r)
}

// Health check server is healthy | Health 检查服务是否健康.
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
