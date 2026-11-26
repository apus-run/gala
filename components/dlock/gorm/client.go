package gorm

import (
	"context"

	"gorm.io/gorm"

	"github.com/apus-run/gala/components/dlock"
)

// Client creates distributed locks via GORM-backed SQL databases | Client 基于 GORM 的 SQL 数据库创建分布式锁。
type Client struct {
	client *gorm.DB
}

// Ensure Client implements dlock.Client and the optional dlock.Initializer |
// 确保 Client 实现 dlock.Client 以及可选的 dlock.Initializer。
var (
	_ dlock.Client      = (*Client)(nil)
	_ dlock.Initializer = (*Client)(nil)
)

// NewClient wraps a *gorm.DB | NewClient 包装一个 *gorm.DB。
func NewClient(db *gorm.DB) *Client {
	return &Client{
		client: db,
	}
}

// NewLock builds a Locker backed by the database. Schema setup is not done
// here: call Init once before first use.
// NewLock 构建基于数据库的 Locker。这里不建表：首次使用前调用一次 Init。
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

// Init creates or migrates the lock table. Call it once during startup, before
// the first NewLock; it is the only place that performs DDL.
// Init 建表或迁移锁表。启动时、首次 NewLock 前调用一次；它是唯一执行 DDL 的地方。
func (cli *Client) Init(ctx context.Context) error {
	return Init(ctx, cli.client)
}
