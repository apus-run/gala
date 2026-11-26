package noop

import (
	"context"

	"github.com/apus-run/gala/components/dlock"
)

// Client is a no-op dlock Client | Client 是空实现的 dlock Client。
type Client struct{}

// Ensure Client implements dlock.Client | 确保 Client 实现 dlock.Client。
var _ dlock.Client = (*Client)(nil)

// NewClient creates a no-op Client | NewClient 创建空实现的 Client。
func NewClient() *Client {
	return &Client{}
}

// NewLock builds a no-op Locker | NewLock 构建空实现的 Locker。
func (cli *Client) NewLock(ctx context.Context, opts ...dlock.Option) (dlock.Locker, error) {
	locker, err := NewLock(ctx, opts...)
	if err != nil {
		// Return an untyped nil so callers never receive a non-nil interface
		// wrapping a nil *NoopLocke.
		// 返回无类型的 nil，避免调用方拿到包着 nil *NoopLocke 的非 nil 接口。
		return nil, err
	}
	return locker, nil
}
