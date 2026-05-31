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

// ClientOption is a functional option for the client.
type ClientOption func(*clientOptions)

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
		opt(o)
	}
}

// WithDialer sets the dialer for the client.
func WithDialer(dialer *websocket.Dialer) ClientOption {
	return func(o *clientOptions) {
		if dialer == nil {
			return
		}
		o.dialer = dialer
	}
}

// WithRequestHeader sets the request header for the client.
func WithRequestHeader(header http.Header) ClientOption {
	return func(o *clientOptions) {
		o.requestHeader = header
	}
}

// WithPing sets the interval for sending ping message to the server.
func WithPing(interval time.Duration) ClientOption {
	return func(o *clientOptions) {
		o.pingDialInterval = interval
	}
}

// Client is a wrapper of gorilla/websocket.
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

// NewClient creates a new client.
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

// GetConnection returns the connection of the client.
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

// connect the websocket server.
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

// TryReconnect tries to reconnect the websocket server.
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

// ping websocket server, try to reconnect if connection failed.
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

// GetCtx returns the context of the client.
func (c *Client) GetCtx() context.Context {
	return c.ctx
}

// CloseConnection closes the connection.
// Note: if set pingDialInterval, the Close method must be called, otherwise it will cause the goroutine to leak
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
