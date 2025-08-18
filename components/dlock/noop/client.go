package noop

import (
	"context"

	"github.com/apus-run/gala/components/dlock"
)

type Client struct{}

func NewClient() *Client {
	return &Client{}
}

func (cli *Client) NewLock(ctx context.Context, opts ...dlock.Option) (dlock.Locker, error) {
	return NewLock(ctx, opts...), nil
}
