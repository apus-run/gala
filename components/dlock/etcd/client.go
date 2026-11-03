package etcd

import (
	"context"

	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/apus-run/gala/components/dlock"
)

// Client creates distributed locks via etcd | Client 基于 etcd 创建分布式锁。
type Client struct {
	client *clientv3.Client
}

// NewClient wraps an etcd client | NewClient 包装一个 etcd 客户端。
func NewClient(cli *clientv3.Client) *Client {
	return &Client{
		client: cli,
	}
}

// NewLock builds a Locker backed by etcd | NewLock 构建基于 etcd 的 Locker。
func (cli *Client) NewLock(ctx context.Context, opts ...dlock.Option) (dlock.Locker, error) {
	return NewLock(ctx, cli.client, opts...), nil
}
