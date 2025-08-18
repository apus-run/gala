package ws

import (
	"context"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// ServerOption is a functional option for the Server.
type ServerOption func(*serverOptions)

type serverOptions struct {
	responseHeader      http.Header
	upgrader            *WSUpgrader
	noClientPingTimeout time.Duration
}

func defaultServerOptions() *serverOptions {
	return &serverOptions{
		upgrader: NewWSUpgrader(),
	}
}

func (o *serverOptions) apply(opts ...ServerOption) {
	for _, opt := range opts {
		opt(o)
	}
}

// WithResponseHeader sets the response header for the WebSocket upgrade response.
func WithResponseHeader(header http.Header) ServerOption {
	return func(o *serverOptions) {
		o.responseHeader = header
	}
}

// WithUpgrader sets the WebSocket upgrader for the server.
func WithUpgrader(upgrader *WSUpgrader) ServerOption {
	return func(o *serverOptions) {
		o.upgrader = upgrader
	}
}

// WithMaxMessageWaitPeriod sets the maximum waiting period for a message before closing the connection.
// Deprecated: use WithNoClientPingTimeout instead.
func WithMaxMessageWaitPeriod(period time.Duration) ServerOption {
	return func(o *serverOptions) {
		o.noClientPingTimeout = period
	}
}

// WithNoClientPingTimeout sets the timeout for the client to send a ping message, if timeout, the connection will be closed.
func WithNoClientPingTimeout(timeout time.Duration) ServerOption {
	return func(o *serverOptions) {
		o.noClientPingTimeout = timeout
	}
}

// Conn is a WebSocket connection.
type Conn = websocket.Conn

// LoopFunc is the function that is called for each WebSocket connection.
type LoopFunc func(ctx context.Context, conn *Conn)

type Server struct {
	ws *WSUpgrader

	w              http.ResponseWriter
	r              *http.Request
	responseHeader http.Header

	// If it is greater than 0, it means that the message waiting timeout mechanism is enabled
	// and the connection will be closed after the timeout, if it is 0, it means that the message
	// waiting timeout mechanism is not enabled.
	noClientPingTimeout time.Duration

	loopFunc LoopFunc
}

// NewServer creates a new WebSocket server.
func NewServer(w http.ResponseWriter, r *http.Request, loopFunc LoopFunc, opts ...ServerOption) *Server {
	o := defaultServerOptions()
	o.apply(opts...)

	return &Server{
		w:        w,
		r:        r,
		loopFunc: loopFunc,

		ws:                  o.upgrader,
		responseHeader:      o.responseHeader,
		noClientPingTimeout: o.noClientPingTimeout,
	}
}

// Run runs the WebSocket server.
func (s *Server) Run(ctx context.Context) error {
	conn, err := s.ws.upgrader.Upgrade(s.w, s.r, s.responseHeader)
	if err != nil {
		return err
	}
	defer conn.Close() //nolint

	if s.noClientPingTimeout > 0 {
		// Set initial read deadline
		if err = conn.SetReadDeadline(time.Now().Add(s.noClientPingTimeout)); err != nil {
			return err
		}

		// Set up Ping handling for the connection,
		// when the client sends a ping message, the server side triggers this callback function
		conn.SetPingHandler(func(string) error {
			return conn.SetReadDeadline(time.Now().Add(s.noClientPingTimeout))
		})
	}

	s.loopFunc(ctx, conn)

	return nil
}
