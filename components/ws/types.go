package ws

import (
	"context"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// Upgrader interface for upgrading HTTP connections to WebSocket connections.
//
//go:generate mockgen -source=./types.go -package=mocks -destination=mocks/ws.mock.go Upgrader
type Upgrader interface {
	// Upgrade upgrades the HTTP server connection to the WebSocket protocol.
	Upgrade(w http.ResponseWriter, r *http.Request, responseHeader http.Header) (*websocket.Conn, error)
}

type Dialer interface {
	// DialContext creates a client connection to the websocket server.
	DialContext(ctx context.Context, url string, h http.Header) (*websocket.Conn, *http.Response, error)
}

// ConnConfigureFunc is used to configure a websocket connection with
// custom handlers. The cancel function cancels the request context when
// invoked in the configure function.
type ConnConfigureFunc func(conn *websocket.Conn, cancel context.CancelFunc) *websocket.Conn

// Option is a function type that applies a configuration to the concrete Upgrader.
type Option interface {
	apply(*websocket.Upgrader)
}

// OptionFunc adapts a plain function to Option.
type OptionFunc func(*websocket.Upgrader)

func (f OptionFunc) apply(o *websocket.Upgrader) {
	f(o)
}

// DefaultOptions returns a new Upgrader holding the package defaults.
func DefaultOptions() *websocket.Upgrader {
	return &websocket.Upgrader{
		HandshakeTimeout: 10 * time.Second,
	}
}

// DefaultUpgrader returns a new Upgrader with the package defaults.
//
// Deprecated: use DefaultOptions.
func DefaultUpgrader() *websocket.Upgrader {
	return DefaultOptions()
}

// NewOptions builds an Upgrader from the defaults, then applies opts in order.
func NewOptions(opts ...Option) *websocket.Upgrader {
	u := DefaultOptions()
	applyUpgrader(u, opts...)
	return u
}

// applyUpgrader 按顺序原地应用选项，不重置默认值、不做校验。
func applyUpgrader(u *websocket.Upgrader, opts ...Option) {
	for _, o := range opts {
		o.apply(u)
	}
}

// WithHandshakeTimeout sets the HandshakeTimeout option.
func WithHandshakeTimeout(t time.Duration) Option {
	return OptionFunc(func(u *websocket.Upgrader) {
		u.HandshakeTimeout = t
	})
}

// WithReadBufferSize sets the ReadBufferSize option.
func WithReadBufferSize(size int) Option {
	return OptionFunc(func(u *websocket.Upgrader) {
		u.ReadBufferSize = size
	})
}

// WithWriteBufferSize sets the WriteBufferSize option.
func WithWriteBufferSize(size int) Option {
	return OptionFunc(func(u *websocket.Upgrader) {
		u.WriteBufferSize = size
	})
}

// WithSubprotocols sets the Subprotocols option.
func WithSubprotocols(subprotocols ...string) Option {
	return OptionFunc(func(u *websocket.Upgrader) {
		u.Subprotocols = subprotocols
	})
}

// WithError sets the Error handler option.
func WithError(fn func(w http.ResponseWriter, r *http.Request, status int, reason error)) Option {
	return OptionFunc(func(u *websocket.Upgrader) {
		u.Error = fn
	})
}

// WithCheckOrigin sets the CheckOrigin handler option.
func WithCheckOrigin(fn func(r *http.Request) bool) Option {
	return OptionFunc(func(u *websocket.Upgrader) {
		u.CheckOrigin = fn
	})
}

// WithCompression enables compression.
func WithCompression() Option {
	return OptionFunc(func(u *websocket.Upgrader) {
		u.EnableCompression = true
	})
}

// WSUpgrader is a wrapper of gorilla/websocket.Upgrader.
type WSUpgrader struct {
	upgrader Upgrader
}

// NewWSUpgrader creates a new WSUpgrader.
func NewWSUpgrader(opts ...Option) *WSUpgrader {
	options := NewOptions(opts...)

	return &WSUpgrader{
		upgrader: options,
	}
}
