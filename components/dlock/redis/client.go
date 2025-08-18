package redis

import (
	"context"

	"github.com/redis/go-redis/v9"

	"github.com/apus-run/gala/components/dlock"
)

type Client struct {
	client *redis.Client
}

func NewClient(cli *redis.Client) *Client {
	return &Client{
		client: cli,
	}
}

func (cli *Client) NewLock(ctx context.Context, opts ...dlock.Option) (dlock.Locker, error) {
	return NewLock(ctx, cli.client, opts...), nil
}
