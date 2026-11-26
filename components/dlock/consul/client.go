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

// Ensure Client implements dlock.Client | 确保 Client 实现 dlock.Client。
var _ dlock.Client = (*Client)(nil)

// NewClient wraps a Consul API client | NewClient 包装一个 Consul API 客户端。
func NewClient(cli *api.Client) *Client {
	return &Client{
		client: cli,
	}
}

// NewLock builds a Locker backed by Consul | NewLock 构建基于 Consul 的 Locker。
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
