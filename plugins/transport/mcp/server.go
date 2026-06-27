package mcp

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync/atomic"

	galaserver "github.com/apus-run/gala/server"

	mcpserver "github.com/mark3labs/mcp-go/server"
)

var (
	_ galaserver.Server     = (*Server)(nil)
	_ galaserver.Endpointer = (*Server)(nil)
	_ http.Handler          = (*Server)(nil)
)

// MiddlewareFunc is a function that takes an http.Handler and returns an http.Handler.
type MiddlewareFunc func(http.Handler) http.Handler

// ServerOption is an HTTP server option.
type ServerOption func(*Server)

// Address with server address.
func Address(addr string) ServerOption {
	return func(s *Server) {
		s.address = addr
	}
}

// Endpoint with server address.
func Endpoint(endpoint *url.URL) ServerOption {
	return func(s *Server) {
		s.endpoint = endpoint
	}
}

// Middleware with server middleware.
func Middleware(m MiddlewareFunc) ServerOption {
	return func(s *Server) {
		s.middleware = m
	}
}

// SrvOptions with server options.
func SrvOptions(opts ...mcpserver.ServerOption) ServerOption {
	return func(s *Server) {
		s.srvOpts = append(s.srvOpts, opts...)
	}
}

// SSEOptions with server SSE options.
func SSEOptions(opts ...mcpserver.SSEOption) ServerOption {
	return func(s *Server) {
		s.sseOpts = append(s.sseOpts, opts...)
	}
}

// Server is a MCP server.
type Server struct {
	*mcpserver.MCPServer
	srv        *http.Server
	sse        *mcpserver.SSEServer
	lis        net.Listener
	healthy    atomic.Bool
	middleware MiddlewareFunc
	address    string
	endpoint   *url.URL
	srvOpts    []mcpserver.ServerOption
	sseOpts    []mcpserver.SSEOption
}

// NewServer creates a new MCP server.
func NewServer(name, version string, opts ...ServerOption) *Server {
	srv := &Server{
		address:    ":8000",
		middleware: func(next http.Handler) http.Handler { return next },
	}
	for _, o := range opts {
		o(srv)
	}
	srv.MCPServer = mcpserver.NewMCPServer(name, version, srv.srvOpts...)
	srv.srv = &http.Server{Addr: srv.address, Handler: srv.middleware(srv)}
	srv.sse = mcpserver.NewSSEServer(srv.MCPServer, append(srv.sseOpts, mcpserver.WithHTTPServer(srv.srv))...)
	return srv
}

// ServeHTTP implements the http.Handler interface.
func (s *Server) ServeHTTP(res http.ResponseWriter, req *http.Request) {
	s.sse.ServeHTTP(res, req)
}

// Endpoint return a real address to registry endpoint.
// examples:
// - http://127.0.0.1:8000
func (s *Server) Endpoint() (*url.URL, error) {
	if s.endpoint != nil {
		return s.endpoint, nil
	}
	if err := s.listenAndEndpoint(); err != nil {
		return nil, err
	}
	return s.endpoint, nil
}

// Start start the MCP server.
func (s *Server) Start(ctx context.Context) error {
	if err := s.listenAndEndpoint(); err != nil {
		return err
	}

	s.srv.BaseContext = func(net.Listener) context.Context {
		return ctx
	}

	s.healthy.Store(true)
	defer s.healthy.Store(false)

	if err := s.srv.Serve(s.lis); err != nil {
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}
	return nil
}

// Stop stop the MCP server.
func (s *Server) Stop(ctx context.Context) error {
	err := s.sse.Shutdown(ctx)
	if !s.healthy.Load() && s.lis != nil {
		if closeErr := s.lis.Close(); err == nil && !errors.Is(closeErr, net.ErrClosed) {
			err = closeErr
		}
	}
	return err
}

// Health check server is healthy.
func (s *Server) Health() bool {
	return s.healthy.Load()
}

func (s *Server) listenAndEndpoint() error {
	if s.lis == nil {
		lis, err := net.Listen("tcp", s.address)
		if err != nil {
			return err
		}
		s.lis = lis
	}
	if s.endpoint == nil {
		addr, err := extractEndpointHost(s.address, s.lis)
		if err != nil {
			return err
		}
		s.endpoint = &url.URL{Scheme: "http", Host: addr}
	}
	return nil
}

func extractEndpointHost(address string, lis net.Listener) (string, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", err
	}
	if tcpAddr, ok := lis.Addr().(*net.TCPAddr); ok {
		port = strconv.Itoa(tcpAddr.Port)
	}
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port), nil
}
