package noop

import (
	"context"

	"github.com/apus-run/gala/components/dlock"
)

// Client is a no-op dlock Client | Client 是空实现的 dlock Client。
type Client struct{}

// NewClient creates a no-op Client | NewClient 创建空实现的 Client。
func NewClient() *Client {
	return &Client{}
}

// NewLock builds a no-op Locker | NewLock 构建空实现的 Locker。
func (cli *Client) NewLock(ctx context.Context, opts ...dlock.Option) (dlock.Locker, error) {
	return NewLock(ctx, opts...), nil
}
