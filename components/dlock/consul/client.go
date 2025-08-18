package consul

import (
	"context"

	"github.com/hashicorp/consul/api"

	"github.com/apus-run/gala/components/dlock"
)

type Client struct {
	client *api.Client
}

func NewClient(cli *api.Client) *Client {
	return &Client{
		client: cli,
	}
}

func (cli *Client) NewLock(ctx context.Context, opts ...dlock.Option) (dlock.Locker, error) {
	return NewLock(ctx, cli.client, opts...)
}
