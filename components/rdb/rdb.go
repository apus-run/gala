// Package rdb is a thin wrapper around go-redis v9 that exposes the client
// through a Provider | 对 go-redis v9 的薄封装，通过 Provider 暴露客户端.
//
// # Notes | 说明
//
// The wrapper adds no retry, tracing or pool policy of its own: New does not
// connect, Connect verifies the link with a PING bounded by ctx and a 3s
// timeout. Provider.Close is idempotent, and NewFromClient /
// NewFromUniversalClient take over the lifecycle of the client passed in.
// Client, Options, Script and friends are aliases of the go-redis types, so
// code written against go-redis works unchanged. Test helpers live in the
// rdbtest subpackage.
// 封装本身不加重试、链路追踪或连接池策略：New 不建连，Connect 用受 ctx 与 3 秒超时
// 约束的 PING 验证连接。Provider.Close 幂等，NewFromClient /
// NewFromUniversalClient 会接管传入客户端的生命周期。Client、Options、Script 等
// 类型是 go-redis 类型的别名，为 go-redis 写的代码可直接使用。测试辅助在 rdbtest
// 子包。
//
// # Features | 功能
//
// - Constructors from options, URL, or an existing client | 从配置、URL 或已有客户端构造
// - Connect with PING verification and timeout | 带超时的 PING 验证连接
// - Close tolerates nil and double close | Close 容忍 nil 与重复关闭
// - Unwrap recovers the underlying standalone *Client | Unwrap 取回底层独立部署的 *Client
//
// # Example | 示例
//
//	p, err := rdb.Connect(ctx, &rdb.Options{Addr: "localhost:6379"})
//	if err != nil {
//		return err
//	}
//	defer p.Close()
//	val, err := p.DB(ctx).Get(ctx, "key").Result()
package rdb

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const connectTimeout = 3 * time.Second

// NewFromURL creates a Provider from a redis URL, e.g. redis://:pass@host:6379/0 | NewFromURL 从 redis URL（如 redis://:pass@host:6379/0）创建 Provider.
func NewFromURL(url string) (Provider, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("failed to parse redis url: %w", err)
	}
	return New(opts)
}

// New creates a Provider from opts. It does not connect | New 从 opts 创建 Provider，不建立连接.
func New(opts *Options) (Provider, error) {
	if opts == nil {
		return nil, ErrNilOptions
	}
	return &provider{cli: redis.NewClient(opts)}, nil
}

// NewFromClient wraps an existing client. The Provider takes over its
// lifecycle: Provider.Close closes cli.
// NewFromClient 包装一个已有客户端。Provider 会接管其生命周期：Provider.Close 会关闭 cli。
func NewFromClient(cli *Client) (Provider, error) {
	if cli == nil {
		return nil, ErrNilClient
	}
	return &provider{cli: cli}, nil
}

// NewFromUniversalClient wraps a standalone, cluster or sentinel client.
// The Provider takes over its lifecycle: Provider.Close closes cli.
// NewFromUniversalClient 包装单机、集群或哨兵客户端。Provider 会接管其生命周期：
// Provider.Close 会关闭 cli。
func NewFromUniversalClient(cli UniversalClient) (Provider, error) {
	if cli == nil {
		return nil, ErrNilClient
	}
	return &provider{cli: cli}, nil
}

// Unwrap returns the underlying *Client. It reports false when the Provider
// is not backed by a standalone *Client (a mock, or a cluster client).
// Unwrap 返回底层的 *Client。当 Provider 背后不是单机 *Client（mock 或集群客户端）时
// 返回 false。
func Unwrap(cli Provider) (*Client, bool) {
	if p, ok := cli.(*provider); ok {
		if c, ok := p.cli.(*Client); ok && c != nil {
			return c, true
		}
	}
	return nil, false
}

// Close closes rdb, ignoring a nil client and an already closed one | Close 关闭 rdb，忽略 nil 客户端与已关闭的客户端.
func Close(rdb *Client) error {
	if rdb == nil {
		return nil
	}

	if err := rdb.Close(); err != nil && !IsClosedError(err) {
		return err
	}
	return nil
}

// Connect creates a Provider and verifies the connection with a PING.
// The PING is bounded by ctx and, if ctx has no earlier deadline, by a
// 3 second timeout.
// Connect 创建 Provider 并用 PING 验证连接。PING 受 ctx 约束；若 ctx 没有更早的
// deadline，则再受 3 秒超时约束。
func Connect(ctx context.Context, opts *Options) (Provider, error) {
	if opts == nil {
		return nil, ErrNilOptions
	}
	cli := redis.NewClient(opts)

	ctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	if err := cli.Ping(ctx).Err(); err != nil {
		_ = Close(cli)
		return nil, fmt.Errorf("failed to ping redis: %w", err)
	}
	return &provider{cli: cli}, nil
}

// NewScript returns a new Script instance | NewScript 返回一个新的 Script 实例.
func NewScript(script string) *Script {
	return redis.NewScript(script)
}
