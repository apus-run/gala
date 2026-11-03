// Package mcp adapts a mark3labs/mcp-go server to the gala server.Server,
// Endpointer and http.Handler interfaces | mcp 包将 mark3labs/mcp-go 服务适配为
// gala 的 server.Server、Endpointer 与 http.Handler 接口.
//
// # Notes | 说明
//
// Server embeds *mcpserver.MCPServer, so MCP tools are registered directly on
// it. The transport is SSE only; streamable-HTTP and stdio modes of mcp-go are
// not exposed. Start listens on the configured address (default ":8000") and
// Stop shuts the SSE server down, closing the listener too when Start never
// became healthy. Endpoint returns the advertised URL, mapping wildcard hosts
// to 127.0.0.1; an explicit Endpoint option wins over the derived one. One
// middleware wraps the whole HTTP server.
// Server 内嵌 *mcpserver.MCPServer，MCP 工具直接注册其上。传输仅支持 SSE，未暴露
// mcp-go 的 streamable HTTP 与 stdio 模式。Start 在配置地址（默认 ":8000"）上监听，
// Stop 关闭 SSE 服务；Start 从未健康过时还会关闭监听器。Endpoint 返回对外地址，
// 通配 host 映射为 127.0.0.1；显式的 Endpoint 选项优先于推导值。仅支持一个包裹整个
// HTTP 服务的中间件。
//
// # Example | 示例
//
//	srv := mcp.NewServer("calc", "1.0.0", mcp.Address(":8000"))
//	// register tools on srv.MCPServer, then:
//	go srv.Start(ctx)
//	ep, err := srv.Endpoint()
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
// MiddlewareFunc 接收一个 http.Handler 并返回一个 http.Handler.
type MiddlewareFunc func(http.Handler) http.Handler

// ServerOption is an HTTP server option | ServerOption 是 HTTP 服务选项.
type ServerOption interface {
	apply(*Server)
}

// ServerOptionFunc adapts a plain function to ServerOption | ServerOptionFunc
// 把普通函数适配为 ServerOption.
type ServerOptionFunc func(*Server)

func (f ServerOptionFunc) apply(o *Server) {
	f(o)
}

// Address sets the server address | Address 设置服务监听地址.
func Address(addr string) ServerOption {
	return ServerOptionFunc(func(s *Server) {
		s.address = addr
	})
}

// Endpoint sets the advertised endpoint URL, overriding the derived one
// | Endpoint 设置对外注册的 endpoint 地址，优先于推导值.
func Endpoint(endpoint *url.URL) ServerOption {
	return ServerOptionFunc(func(s *Server) {
		s.endpoint = endpoint
	})
}

// Middleware sets the server middleware | Middleware 设置服务中间件.
func Middleware(m MiddlewareFunc) ServerOption {
	return ServerOptionFunc(func(s *Server) {
		s.middleware = m
	})
}

// SrvOptions adds options for the embedded mcp-go MCPServer | SrvOptions
// 追加内嵌 mcp-go MCPServer 的选项.
func SrvOptions(opts ...mcpserver.ServerOption) ServerOption {
	return ServerOptionFunc(func(s *Server) {
		s.srvOpts = append(s.srvOpts, opts...)
	})
}

// SSEOptions adds options for the SSE transport | SSEOptions 追加 SSE
// 传输的选项.
func SSEOptions(opts ...mcpserver.SSEOption) ServerOption {
	return ServerOptionFunc(func(s *Server) {
		s.sseOpts = append(s.sseOpts, opts...)
	})
}

// Server is a MCP server | Server 是一个 MCP 服务.
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

// NewServer creates a new MCP server | NewServer 创建一个新的 MCP 服务.
func NewServer(name, version string, opts ...ServerOption) *Server {
	srv := &Server{
		address:    ":8000",
		middleware: func(next http.Handler) http.Handler { return next },
	}
	for _, o := range opts {
		o.apply(srv)
	}
	srv.MCPServer = mcpserver.NewMCPServer(name, version, srv.srvOpts...)
	srv.srv = &http.Server{Addr: srv.address, Handler: srv.middleware(srv)}
	srv.sse = mcpserver.NewSSEServer(srv.MCPServer, append(srv.sseOpts, mcpserver.WithHTTPServer(srv.srv))...)
	return srv
}

// ServeHTTP implements the http.Handler interface | ServeHTTP 实现 http.Handler
// 接口.
func (s *Server) ServeHTTP(res http.ResponseWriter, req *http.Request) {
	s.sse.ServeHTTP(res, req)
}

// Endpoint return a real address to registry endpoint.
// examples:
// - http://127.0.0.1:8000
//
// Endpoint 返回用于注册的真实地址，例如 http://127.0.0.1:8000.
func (s *Server) Endpoint() (*url.URL, error) {
	if s.endpoint != nil {
		return s.endpoint, nil
	}
	if err := s.listenAndEndpoint(); err != nil {
		return nil, err
	}
	return s.endpoint, nil
}

// Start start the MCP server | Start 启动 MCP 服务.
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

// Stop stop the MCP server | Stop 停止 MCP 服务.
func (s *Server) Stop(ctx context.Context) error {
	err := s.sse.Shutdown(ctx)
	if !s.healthy.Load() && s.lis != nil {
		if closeErr := s.lis.Close(); err == nil && !errors.Is(closeErr, net.ErrClosed) {
			err = closeErr
		}
	}
	return err
}

// Health check server is healthy | Health 检查服务是否健康.
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
