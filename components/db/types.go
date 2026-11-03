//go:generate mockgen -source=$GOFILE -destination=mocks/$GOFILE -package=mocks
package db

import (
	"context"

	"gorm.io/gorm"
)

// Provider wraps gorm sessions and transactions behind a ctx-first interface | Provider 以 ctx 为先，包装 gorm 的会话与事务。
type Provider interface {
	// DB creates a new database session; when ctx is passed in by TX, the session lands on that transaction | DB 创建一个新的数据库会话；ctx 由 TX 传入时，会话落在该事务上。
	DB(ctx context.Context, opts ...Option) *gorm.DB
	// TX runs a transaction; inside fn, DB(ctx) returns the transaction session.
	// The transaction ctx must not be used concurrently across goroutines:
	// sql.Tx does not support concurrent queries.
	// TX 执行一个事务，fn 内用 DB(ctx) 取得事务会话。
	// 事务 ctx 不能跨 goroutine 并发使用：sql.Tx 不支持并发查询。
	TX(ctx context.Context, fn func(ctx context.Context) error, opts ...Option) error
	// Close closes the database connection | Close 关闭数据库连接。
	Close() error
}

type (
	// Option configures a DB call | Option 配置一次 DB 调用。
	Option interface {
		apply(*Options)
	}

	// OptionFunc adapts a plain function to Option | OptionFunc 把普通函数适配为 Option。
	OptionFunc func(*Options)

	// Options holds the per-call settings for DB | Options 保存 DB 每次调用的设置。
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

// DefaultOptions returns a new zero-valued Options | DefaultOptions 返回一个新的零值 Options。
func DefaultOptions() *Options {
	return &Options{}
}

// NewOptions builds Options from the defaults, then applies opts in order | NewOptions 从默认值构造 Options，再按顺序应用 opts。
func NewOptions(opts ...Option) *Options {
	o := DefaultOptions()
	o.Apply(opts...)
	return o
}

// Apply applies opts in place in order, without resetting defaults or validating | Apply 按顺序原地应用选项，不重置默认值、不做校验。
func (o *Options) Apply(opts ...Option) {
	for _, opt := range opts {
		opt.apply(o)
	}
}

// WithMaster forces reads to the primary | WithMaster 强制读主库。
func WithMaster() Option {
	return OptionFunc(func(o *Options) {
		o.withMaster = true
	})
}

// WithTransaction uses an existing transaction | WithTransaction 使用一个已有的事务。
func WithTransaction(tx *gorm.DB) Option {
	return OptionFunc(func(o *Options) {
		o.tx = tx
	})
}

// Debug enables debug mode | Debug 启用调试模式。
func Debug() Option {
	return OptionFunc(func(o *Options) {
		o.debug = true
	})
}

// WithDeleted returns soft-deleted rows | WithDeleted 返回软删的数据。
func WithDeleted() Option {
	return OptionFunc(func(o *Options) {
		o.withDeleted = true
	})
}

// WithSelectForUpdate locks the rows it reads with SELECT ... FOR UPDATE.
// Note: it only takes effect inside a transaction.
// WithSelectForUpdate 使用 SELECT ... FOR UPDATE 锁定读取的行
// 注意：只有在事务中使用才有效
func WithSelectForUpdate() Option {
	return OptionFunc(func(o *Options) {
		o.selectForUpdate = true
	})
}
