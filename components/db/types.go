// Package db package provides an interface for database operations
//
//go:generate mockgen -source=$GOFILE -destination=mocks/$GOFILE -package=mocks
package db

import (
	"context"

	"gorm.io/gorm"
)

type Provider interface {
	// DB 创建一个新的数据库会话；ctx 由 TX 传入时，会话落在该事务上
	DB(ctx context.Context, opts ...Option) *gorm.DB
	// TX 执行一个事务，fn 内用 DB(ctx) 取得事务会话。
	// 事务 ctx 不能跨 goroutine 并发使用：sql.Tx 不支持并发查询。
	TX(ctx context.Context, fn func(ctx context.Context) error, opts ...Option) error
	// Close 关闭数据库连接
	Close() error
}

type (
	// Option configures a DB call.
	Option interface {
		apply(*Options)
	}

	// OptionFunc adapts a plain function to Option.
	OptionFunc func(*Options)

	Options struct {
		tx              *gorm.DB
		debug           bool
		withMaster      bool
		withDeleted     bool
		selectForUpdate bool
	}
)

func (f OptionFunc) apply(o *Options) {
	f(o)
}

// DefaultOptions returns a new zero-valued Options.
func DefaultOptions() *Options {
	return &Options{}
}

// NewOptions builds Options from the defaults, then applies opts in order.
func NewOptions(opts ...Option) *Options {
	o := DefaultOptions()
	o.Apply(opts...)
	return o
}

// Apply 按顺序原地应用选项，不重置默认值、不做校验。
func (o *Options) Apply(opts ...Option) {
	for _, opt := range opts {
		opt.apply(o)
	}
}

// WithMaster 强制读主库
func WithMaster() Option {
	return OptionFunc(func(o *Options) {
		o.withMaster = true
	})
}

// WithTransaction 使用一个已有的事务
func WithTransaction(tx *gorm.DB) Option {
	return OptionFunc(func(o *Options) {
		o.tx = tx
	})
}

// Debug 启用调试模式
func Debug() Option {
	return OptionFunc(func(o *Options) {
		o.debug = true
	})
}

// WithDeleted 返回软删的数据
func WithDeleted() Option {
	return OptionFunc(func(o *Options) {
		o.withDeleted = true
	})
}

// WithSelectForUpdate 使用 SELECT ... FOR UPDATE 锁定读取的行
// 注意：只有在事务中使用才有效
func WithSelectForUpdate() Option {
	return OptionFunc(func(o *Options) {
		o.selectForUpdate = true
	})
}
