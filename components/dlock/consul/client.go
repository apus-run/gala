package consul

import (
	"context"

	"github.com/hashicorp/consul/api"

	"github.com/apus-run/gala/components/dlock"
)

// Client creates distributed locks via Consul | Client 基于 Consul 创建分布式锁。
type Client struct {
	client *api.Client
}

// NewClient wraps a Consul API client | NewClient 包装一个 Consul API 客户端。
func NewClient(cli *api.Client) *Client {
	return &Client{
		client: cli,
	}
}

// NewLock builds a Locker backed by Consul | NewLock 构建基于 Consul 的 Locker。
func (cli *Client) NewLock(ctx context.Context, opts ...dlock.Option) (dlock.Locker, error) {
	return NewLock(ctx, cli.client, opts...)
}
