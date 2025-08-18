package ws

import (
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// Upgrader interface for upgrading HTTP connections to WebSocket connections.
//
//go:generate mockgen -source=./types.go -package=mocks -destination=mocks/ws.mock.go Upgrader
type Upgrader interface {
	Upgrade(w http.ResponseWriter, r *http.Request, responseHeader http.Header) (*websocket.Conn, error)
}

// Option is a function type that applies a configuration to the concrete Upgrader.
type Option func(u *websocket.Upgrader)

func DefaultUpgrader() *websocket.Upgrader {
	return &websocket.Upgrader{
		// 默认支持跨域
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
}

func Apply(opts ...Option) *websocket.Upgrader {
	options := DefaultUpgrader()
	for _, o := range opts {
		o(options)
	}
	return options
}

// WithHandshakeTimeout sets the HandshakeTimeout option.
func WithHandshakeTimeout(t time.Duration) Option {
	return func(u *websocket.Upgrader) {
		u.HandshakeTimeout = t
	}
}

// WithReadBufferSize sets the ReadBufferSize option.
func WithReadBufferSize(size int) Option {
	return func(u *websocket.Upgrader) {
		u.ReadBufferSize = size
	}
}

// WithWriteBufferSize sets the WriteBufferSize option.
func WithWriteBufferSize(size int) Option {
	return func(u *websocket.Upgrader) {
		u.WriteBufferSize = size
	}
}

// WithSubprotocols sets the Subprotocols option.
func WithSubprotocols(subprotocols ...string) Option {
	return func(u *websocket.Upgrader) {
		u.Subprotocols = subprotocols
	}
}

// WithError sets the Error handler option.
func WithError(fn func(w http.ResponseWriter, r *http.Request, status int, reason error)) Option {
	return func(u *websocket.Upgrader) {
		u.Error = fn
	}
}

// WithCheckOrigin sets the CheckOrigin handler option.
func WithCheckOrigin(fn func(r *http.Request) bool) Option {
	return func(u *websocket.Upgrader) {
		u.CheckOrigin = fn
	}
}

// WithCompression enables compression.
func WithCompression() Option {
	return func(u *websocket.Upgrader) {
		u.EnableCompression = true
	}
}

// WSUpgrader is a wrapper of gorilla/websocket.Upgrader.
type WSUpgrader struct {
	upgrader Upgrader
}

// NewWSUpgrader creates a new WSUpgrader.
func NewWSUpgrader(opts ...Option) *WSUpgrader {
	options := Apply(opts...)

	return &WSUpgrader{
		upgrader: options,
	}
}
