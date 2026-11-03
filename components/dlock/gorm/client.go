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

// NewClient wraps a *gorm.DB | NewClient 包装一个 *gorm.DB。
func NewClient(db *gorm.DB) *Client {
	return &Client{
		client: db,
	}
}

// NewLock builds a Locker backed by the database | NewLock 构建基于数据库的 Locker。
func (cli *Client) NewLock(ctx context.Context, opts ...dlock.Option) (dlock.Locker, error) {
	return NewLock(ctx, cli.client, opts...)
}
