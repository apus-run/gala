// Package db package provides an interface for database operations
//
//go:generate mockgen -source=$GOFILE -destination=mocks/$GOFILE -package=mocks
package db

import (
	"context"

	"gorm.io/gorm"
)

type Provider interface {
	// DB 创建一个新的数据库会话
	DB(ctx context.Context, opts ...Option) *gorm.DB
	// TX 执行一个事务
	TX(ctx context.Context, fn func(tx *gorm.DB) error, opts ...Option) error
}

type Options struct {
	tx          *gorm.DB // 数据库事务
	debug       bool     // 调试模式
	withMaster  bool     // 强制读主库
	withDeleted bool     // 返回软删的数据
	forUpdate   bool     // 使用 SELECT ... FOR UPDATE 锁定读取的行
}

type Option func(*Options)

func Apply(opts ...Option) *Options {
	o := &Options{}
	for _, fn := range opts {
		fn(o)
	}
	return o
}

// WithMaster 强制读主库
func WithMaster() Option {
	return func(o *Options) {
		o.withMaster = true
	}
}

// WithTransaction 使用一个已有的事务
func WithTransaction(tx *gorm.DB) Option {
	return func(o *Options) {
		o.tx = tx
	}
}

// Debug 启用调试模式
func Debug() Option {
	return func(o *Options) {
		o.debug = true
	}
}

// WithDeleted 返回软删的数据
func WithDeleted() Option {
	return func(o *Options) {
		o.withDeleted = true
	}
}

// WithSelectForUpdate 使用 SELECT ... FOR UPDATE 锁定读取的行
// 注意：只有在事务中使用才有效
func WithSelectForUpdate() Option {
	return func(o *Options) {
		o.forUpdate = true
	}
}

// ContainWithMasterOpt 检查选项中是否包含强制读主库的选项
// 用于在执行查询前检查是否需要强制读主库
func ContainWithMasterOpt(opts []Option) bool {
	o := &Options{}
	for _, fn := range opts {
		fn(o)
		if o.withMaster {
			return true
		}
	}
	return false
}
