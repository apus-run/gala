package redis

import (
	"context"

	"github.com/redis/go-redis/v9"

	"github.com/apus-run/gala/components/dlock"
)

// Client creates distributed locks via Redis | Client 基于 Redis 创建分布式锁。
type Client struct {
	client *redis.Client
}

// NewClient wraps a go-redis client | NewClient 包装一个 go-redis 客户端。
func NewClient(cli *redis.Client) *Client {
	return &Client{
		client: cli,
	}
}

// NewLock builds a Locker backed by Redis | NewLock 构建基于 Redis 的 Locker。
func (cli *Client) NewLock(ctx context.Context, opts ...dlock.Option) (dlock.Locker, error) {
	return NewLock(ctx, cli.client, opts...), nil
}
