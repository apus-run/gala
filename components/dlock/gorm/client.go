package gorm

import (
	"context"

	"gorm.io/gorm"

	"github.com/apus-run/gala/components/dlock"
)

type Client struct {
	client *gorm.DB
}

func NewClient(db *gorm.DB) *Client {
	return &Client{
		client: db,
	}
}

func (cli *Client) NewLock(ctx context.Context, opts ...dlock.Option) (dlock.Locker, error) {
	return NewLock(ctx, cli.client, opts...)
}
