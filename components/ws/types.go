package ws

import (
	"context"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// Upgrader interface for upgrading HTTP connections to WebSocket connections | 将 HTTP 连接升级为 WebSocket 连接的接口.
//
//go:generate mockgen -source=./types.go -package=mocks -destination=mocks/ws.mock.go Upgrader
type Upgrader interface {
	// Upgrade upgrades the HTTP server connection to the WebSocket protocol | Upgrade 将 HTTP 服务端连接升级为 WebSocket 协议.
	Upgrade(w http.ResponseWriter, r *http.Request, responseHeader http.Header) (*websocket.Conn, error)
}

// Dialer dials client websocket connections | Dialer 创建客户端 websocket 连接.
type Dialer interface {
	// DialContext creates a client connection to the websocket server | DialContext 创建到 websocket 服务端的客户端连接.
	DialContext(ctx context.Context, url string, h http.Header) (*websocket.Conn, *http.Response, error)
}

// ConnConfigureFunc is used to configure a websocket connection with
// custom handlers. The cancel function cancels the request context when
// invoked in the configure function.
// ConnConfigureFunc 用自定义处理器配置 websocket 连接；cancel 在配置函数中被调用时
// 取消请求的 ctx.
type ConnConfigureFunc func(conn *websocket.Conn, cancel context.CancelFunc) *websocket.Conn

// Option is a function type that applies a configuration to the concrete Upgrader | Option 为具体 Upgrader 应用配置的函数类型.
type Option interface {
	apply(*websocket.Upgrader)
}

// OptionFunc adapts a plain function to Option | OptionFunc 把普通函数适配为 Option.
type OptionFunc func(*websocket.Upgrader)

// apply runs the adapted function | apply 执行被适配的函数.
func (f OptionFunc) apply(o *websocket.Upgrader) {
	f(o)
}

// DefaultOptions returns a new Upgrader holding the package defaults | DefaultOptions 返回持有包默认值的 Upgrader.
func DefaultOptions() *websocket.Upgrader {
	return &websocket.Upgrader{
		HandshakeTimeout: 10 * time.Second,
	}
}

// DefaultUpgrader returns a new Upgrader with the package defaults | DefaultUpgrader 返回带包默认值的 Upgrader.
//
// Deprecated: use DefaultOptions | 已废弃，改用 DefaultOptions.
func DefaultUpgrader() *websocket.Upgrader {
	return DefaultOptions()
}

// NewOptions builds an Upgrader from the defaults, then applies opts in order | NewOptions 从默认值出发按顺序应用 opts 构造 Upgrader.
func NewOptions(opts ...Option) *websocket.Upgrader {
	u := DefaultOptions()
	applyUpgrader(u, opts...)
	return u
}

// applyUpgrader 按顺序原地应用选项，不重置默认值、不做校验。
// applyUpgrader applies the options in place, in order; it neither resets defaults nor validates.
func applyUpgrader(u *websocket.Upgrader, opts ...Option) {
	for _, o := range opts {
		o.apply(u)
	}
}

// WithHandshakeTimeout sets the HandshakeTimeout option | WithHandshakeTimeout 设置 HandshakeTimeout 选项.
func WithHandshakeTimeout(t time.Duration) Option {
	return OptionFunc(func(u *websocket.Upgrader) {
		u.HandshakeTimeout = t
	})
}

// WithReadBufferSize sets the ReadBufferSize option | WithReadBufferSize 设置 ReadBufferSize 选项.
func WithReadBufferSize(size int) Option {
	return OptionFunc(func(u *websocket.Upgrader) {
		u.ReadBufferSize = size
	})
}

// WithWriteBufferSize sets the WriteBufferSize option | WithWriteBufferSize 设置 WriteBufferSize 选项.
func WithWriteBufferSize(size int) Option {
	return OptionFunc(func(u *websocket.Upgrader) {
		u.WriteBufferSize = size
	})
}

// WithSubprotocols sets the Subprotocols option | WithSubprotocols 设置 Subprotocols 选项.
func WithSubprotocols(subprotocols ...string) Option {
	return OptionFunc(func(u *websocket.Upgrader) {
		u.Subprotocols = subprotocols
	})
}

// WithError sets the Error handler option | WithError 设置 Error 处理器选项.
func WithError(fn func(w http.ResponseWriter, r *http.Request, status int, reason error)) Option {
	return OptionFunc(func(u *websocket.Upgrader) {
		u.Error = fn
	})
}

// WithCheckOrigin sets the CheckOrigin handler option | WithCheckOrigin 设置 CheckOrigin 处理器选项.
func WithCheckOrigin(fn func(r *http.Request) bool) Option {
	return OptionFunc(func(u *websocket.Upgrader) {
		u.CheckOrigin = fn
	})
}

// WithCompression enables compression | WithCompression 开启压缩.
func WithCompression() Option {
	return OptionFunc(func(u *websocket.Upgrader) {
		u.EnableCompression = true
	})
}

// WSUpgrader is a wrapper of gorilla/websocket.Upgrader | WSUpgrader 是 gorilla/websocket.Upgrader 的封装.
type WSUpgrader struct {
	upgrader Upgrader
}

// NewWSUpgrader creates a new WSUpgrader | NewWSUpgrader 创建一个新的 WSUpgrader.
func NewWSUpgrader(opts ...Option) *WSUpgrader {
	options := NewOptions(opts...)

	return &WSUpgrader{
		upgrader: options,
	}
}
