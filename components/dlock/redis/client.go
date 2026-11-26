package redis

import (
	"context"

	"github.com/redis/go-redis/v9"

	"github.com/apus-run/gala/components/dlock"
)

// Client creates distributed locks via Redis | Client 基于 Redis 创建分布式锁。
type Client struct {
	client redis.Cmdable
}

// Ensure Client implements dlock.Client | 确保 Client 实现 dlock.Client。
var _ dlock.Client = (*Client)(nil)

// NewClient wraps a go-redis client. redis.Cmdable keeps the client usable
// with *redis.Client, *redis.ClusterClient and test doubles.
// NewClient 包装一个 go-redis 客户端。redis.Cmdable 同时兼容 *redis.Client、
// *redis.ClusterClient 与测试替身。
func NewClient(cli redis.Cmdable) *Client {
	return &Client{
		client: cli,
	}
}

// NewLock builds a Locker backed by Redis | NewLock 构建基于 Redis 的 Locker。
func (cli *Client) NewLock(ctx context.Context, opts ...dlock.Option) (dlock.Locker, error) {
	locker, err := NewLock(ctx, cli.client, opts...)
	if err != nil {
		// Return an untyped nil so callers never receive a non-nil interface
		// wrapping a nil *Lock.
		// 返回无类型的 nil，避免调用方拿到包着 nil *Lock 的非 nil 接口。
		return nil, err
	}
	return locker, nil
}
