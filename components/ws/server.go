// Package ws is a thin WebSocket wrapper around gorilla/websocket: a server
// that upgrades and hands the connection to a loop function, plus a client
// that pings and reconnects on its own | 基于 gorilla/websocket 的薄封装：服务端升级连接交给回调，客户端自带心跳与重连.
//
// # Notes | 说明
//
// Server.Run upgrades one HTTP connection and calls the caller's LoopFunc —
// reads, writes and per-message handling stay in caller code.
// WithNoClientPingTimeout closes a connection unless the client sends ping
// control frames; browsers cannot send those, so browser-facing services
// must use application-level heartbeats instead. The client reconnects with
// exponential backoff (1s doubling to 32s) when its ping fails; GetConnection
// panics if a redial attempt fails. Hub-style routing (userID to connections)
// and other IM semantics are deliberately out of scope; see examples/hub.
// Server.Run 只升级一条 HTTP 连接并调用调用方的 LoopFunc —— 读写与逐消息处理都
// 留在调用方。WithNoClientPingTimeout 在客户端不发 ping 控制帧时关闭连接；浏览器
// 发不了 ping，面向浏览器的服务要用应用层心跳。客户端 ping 失败后按指数退避
// （1 秒翻倍到 32 秒）重连；GetConnection 重拨失败会 panic。Hub 式路由（userID 到
// 连接）等 IM 语义刻意不进组件库，见 examples/hub。
//
// # Features | 功能
//
// - Server: upgrade plus Run with a per-connection LoopFunc | Server 升级连接并按连接运行 LoopFunc
// - Client: dial, ping interval, reconnect with backoff | 客户端拨号、心跳间隔与退避重连
// - WSUpgrader wraps the gorilla upgrader with options | WSUpgrader 带选项封装 gorilla 升级器
// - Upgrader / Dialer interfaces for mocking | 可 mock 的 Upgrader / Dialer 接口
//
// # Example | 示例
//
//	s := ws.NewServer(w, r, loopFunc)
//	if err := s.Run(ctx); err != nil {
//		log.Println("ws server error:", err)
//	}
package ws

import (
	"context"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// ServerOption is a functional option for the Server | ServerOption 是 Server 的函数式选项.
type ServerOption interface {
	apply(*serverOptions)
}

// ServerOptionFunc adapts a plain function to ServerOption | ServerOptionFunc 把普通函数适配为 ServerOption.
type ServerOptionFunc func(*serverOptions)

// apply runs the adapted function | apply 执行被适配的函数.
func (f ServerOptionFunc) apply(o *serverOptions) {
	f(o)
}

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
		opt.apply(o)
	}
}

// WithResponseHeader sets the response header for the WebSocket upgrade response | WithResponseHeader 设置 WebSocket 升级响应的响应头.
func WithResponseHeader(header http.Header) ServerOption {
	return ServerOptionFunc(func(o *serverOptions) {
		o.responseHeader = header
	})
}

// WithUpgrader sets the WebSocket upgrader for the server | WithUpgrader 设置服务端的 WebSocket 升级器.
func WithUpgrader(upgrader *WSUpgrader) ServerOption {
	return ServerOptionFunc(func(o *serverOptions) {
		if upgrader == nil {
			return
		}
		o.upgrader = upgrader
	})
}

// WithMaxMessageWaitPeriod sets the maximum waiting period for a message before closing the connection | WithMaxMessageWaitPeriod 设置关闭连接前等待消息的最长时间.
// Deprecated: use WithNoClientPingTimeout instead | 已废弃，改用 WithNoClientPingTimeout.
func WithMaxMessageWaitPeriod(period time.Duration) ServerOption {
	return ServerOptionFunc(func(o *serverOptions) {
		o.noClientPingTimeout = period
	})
}

// WithNoClientPingTimeout sets the timeout for the client to send a ping message, if timeout, the connection will be closed.
// 设置客户端多久内必须发一次 ping，超时即关闭连接.
//
// Only ping control frames from the client renew the read deadline; ordinary application
// messages do not: a connection that keeps sending data but never pings is still closed
// after the timeout. Browser JS cannot send ping control frames, so browser-facing
// services must not set this option and should use application-level heartbeats instead;
// the gala ws client can simply use ws.WithPing.
// 只有客户端发来的 ping 控制帧会续期读超时，普通的应用层消息不会：一个一直在发数据、但不发 ping 的
// 连接，也会在 timeout 之后被关闭。浏览器 JS 发不了 ping 控制帧，面向浏览器的服务不要设置这个选项，
// 改用应用层心跳；gala 的 ws 客户端用 ws.WithPing 即可。
func WithNoClientPingTimeout(timeout time.Duration) ServerOption {
	return ServerOptionFunc(func(o *serverOptions) {
		o.noClientPingTimeout = timeout
	})
}

// Conn is a WebSocket connection | Conn 是一条 WebSocket 连接.
type Conn = websocket.Conn

// LoopFunc is the function that is called for each WebSocket connection | LoopFunc 是每条 WebSocket 连接上调用的函数.
type LoopFunc func(ctx context.Context, conn *Conn)

// Server upgrades one HTTP connection and hands it to the loop function | Server 升级一条 HTTP 连接并交给循环函数.
type Server struct {
	ws *WSUpgrader

	w              http.ResponseWriter
	r              *http.Request
	responseHeader http.Header

	// If it is greater than 0, it means that the message waiting timeout mechanism is enabled
	// and the connection will be closed after the timeout, if it is 0, it means that the message
	// waiting timeout mechanism is not enabled.
	// 大于 0 表示启用消息等待超时机制，超时后关闭连接；为 0 表示不启用.
	noClientPingTimeout time.Duration

	loopFunc LoopFunc
}

// NewServer creates a new WebSocket server | NewServer 创建一个新的 WebSocket 服务端.
func NewServer(w http.ResponseWriter, r *http.Request, loopFunc LoopFunc, opts ...ServerOption) *Server {
	o := defaultServerOptions()
	o.apply(opts...)
	if loopFunc == nil {
		loopFunc = func(context.Context, *Conn) {}
	}

	return &Server{
		w:        w,
		r:        r,
		loopFunc: loopFunc,

		ws:                  o.upgrader,
		responseHeader:      o.responseHeader,
		noClientPingTimeout: o.noClientPingTimeout,
	}
}

// Run runs the WebSocket server | Run 运行 WebSocket 服务端.
func (s *Server) Run(ctx context.Context) error {
	conn, err := s.ws.upgrader.Upgrade(s.w, s.r, s.responseHeader)
	if err != nil {
		return err
	}
	defer conn.Close() //nolint

	if s.noClientPingTimeout > 0 {
		// Set initial read deadline
		// 设置初始读超时
		if err = conn.SetReadDeadline(time.Now().Add(s.noClientPingTimeout)); err != nil {
			return err
		}

		// Set up Ping handling for the connection,
		// when the client sends a ping message, the server side triggers this callback function
		// 为连接设置 ping 处理：客户端发来 ping 消息时服务端触发该回调
		pingHandler := conn.PingHandler()
		conn.SetPingHandler(func(appData string) error {
			if err := conn.SetReadDeadline(time.Now().Add(s.noClientPingTimeout)); err != nil {
				return err
			}
			return pingHandler(appData)
		})
	}

	s.loopFunc(ctx, conn)

	return nil
}
