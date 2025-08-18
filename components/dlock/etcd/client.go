package etcd

import (
	"context"

	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/apus-run/gala/components/dlock"
)

type Client struct {
	client *clientv3.Client
}

func NewClient(cli *clientv3.Client) *Client {
	return &Client{
		client: cli,
	}
}

func (cli *Client) NewLock(ctx context.Context, opts ...dlock.Option) (dlock.Locker, error) {
	return NewLock(ctx, cli.client, opts...), nil
}
