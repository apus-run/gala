package ws

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var (
	pingData = []byte("ping")
)

// ClientOption is a functional option for the client | ClientOption 是客户端的函数式选项.
type ClientOption interface {
	apply(*clientOptions)
}

// ClientOptionFunc adapts a plain function to ClientOption | ClientOptionFunc 把普通函数适配为 ClientOption.
type ClientOptionFunc func(*clientOptions)

// apply runs the adapted function | apply 执行被适配的函数.
func (f ClientOptionFunc) apply(o *clientOptions) {
	f(o)
}

type clientOptions struct {
	dialer           *websocket.Dialer
	requestHeader    http.Header
	pingDialInterval time.Duration
}

func defaultClientOptions() *clientOptions {
	return &clientOptions{
		dialer: websocket.DefaultDialer,
	}
}

func (o *clientOptions) apply(opts ...ClientOption) {
	for _, opt := range opts {
		opt.apply(o)
	}
}

// WithDialer sets the dialer for the client | WithDialer 设置客户端的拨号器.
func WithDialer(dialer *websocket.Dialer) ClientOption {
	return ClientOptionFunc(func(o *clientOptions) {
		if dialer == nil {
			return
		}
		o.dialer = dialer
	})
}

// WithRequestHeader sets the request header for the client | WithRequestHeader 设置客户端的请求头.
func WithRequestHeader(header http.Header) ClientOption {
	return ClientOptionFunc(func(o *clientOptions) {
		o.requestHeader = header
	})
}

// WithPing sets the interval for sending ping message to the server | WithPing 设置向服务端发送 ping 的间隔.
func WithPing(interval time.Duration) ClientOption {
	return ClientOptionFunc(func(o *clientOptions) {
		o.pingDialInterval = interval
	})
}

// Client is a wrapper of gorilla/websocket, with its own ping and reconnect | Client 是 gorilla/websocket 的封装，自带心跳与重连.
type Client struct {
	dialer        *websocket.Dialer
	requestHeader http.Header
	url           string

	mu   sync.RWMutex
	conn *websocket.Conn

	pingInterval time.Duration
	ctx          context.Context
	cancel       context.CancelFunc
}

// NewClient creates a new client | NewClient 创建一个新的客户端.
func NewClient(url string, opts ...ClientOption) (*Client, error) {
	o := defaultClientOptions()
	o.apply(opts...)

	ctx, cancel := context.WithCancel(context.Background())

	c := &Client{
		url:           url,
		dialer:        o.dialer,
		requestHeader: o.requestHeader,
		pingInterval:  o.pingDialInterval,
		ctx:           ctx,
		cancel:        cancel,
	}

	err := c.connect()
	if err != nil {
		return nil, err
	}

	if c.pingInterval > 0 {
		c.ping()

	}

	return c, nil
}

// GetConnection returns the connection of the client. It redials and panics on failure
// when no connection is available.
// GetConnection 返回客户端的连接。当前没有连接时会重拨一次，失败即 panic.
func (c *Client) GetConnection() *websocket.Conn {
	conn := c.getConnection()
	if conn == nil {
		err := c.connect()
		if err != nil {
			panic(err)
		}
		conn = c.getConnection()
	}

	return conn
}

func (c *Client) getConnection() *websocket.Conn {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.conn
}

// connect the websocket server | connect 连接 websocket 服务端.
func (c *Client) connect() error {
	conn, _, err := c.dialer.DialContext(c.ctx, c.url, c.requestHeader)
	if err != nil {
		return err
	}

	c.mu.Lock()
	if err := c.ctx.Err(); err != nil {
		c.mu.Unlock()
		_ = conn.Close()
		return err
	}
	oldConn := c.conn
	c.conn = conn
	c.mu.Unlock()

	if oldConn != nil {
		_ = oldConn.Close()
	}
	return nil
}

// TryReconnect tries to reconnect the websocket server, backing off exponentially from
// 1s up to 32s until the dial succeeds or the client context is done.
// TryReconnect 尝试重连 websocket 服务端，从 1 秒起按指数退避到 32 秒，直到拨号成功
// 或客户端 ctx 结束.
func (c *Client) TryReconnect() error {
	delay := 1 * time.Second
	maxDelay := 32 * time.Second
	for {
		select {
		case <-c.ctx.Done():
			return c.ctx.Err()
		case <-time.After(delay):
			if err := c.connect(); err != nil {
				if delay >= maxDelay {
					delay = maxDelay

					continue
				}
				delay *= 2
				continue
			}

			return nil
		}
	}
}

// ping websocket server, try to reconnect if connection failed | ping 服务端，连接失败时尝试重连.
func (c *Client) ping() {
	go func() {
		isExit := false
		defer func() {
			if e := recover(); e != nil {
				panic(e)
			}

			if !isExit {
				if err := c.TryReconnect(); err == nil {
					c.ping()
				}
			}
		}()

		ticker := time.NewTicker(c.pingInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				conn := c.getConnection()
				if conn == nil {
					return
				}
				if err := conn.WriteControl(websocket.PingMessage, pingData, time.Now().Add(5*time.Second)); err != nil {

					return
				}

			case <-c.ctx.Done(): // exit
				isExit = true
				return
			}
		}
	}()
}

// GetCtx returns the context of the client | GetCtx 返回客户端的 ctx.
func (c *Client) GetCtx() context.Context {
	return c.ctx
}

// CloseConnection closes the connection.
// Note: if set pingDialInterval, the Close method must be called, otherwise it will cause the goroutine to leak
// CloseConnection 关闭连接。注意：设置过 pingDialInterval 时必须调用本方法，否则会泄漏协程.
func (c *Client) CloseConnection() error {
	if c.cancel != nil {
		c.cancel()
	}
	conn := c.getConnection()
	if conn != nil {
		return conn.Close()
	}

	return nil
}
