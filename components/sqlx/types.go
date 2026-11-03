package sqlx

import (
	"context"
	"database/sql"
	"io"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/jmoiron/sqlx/reflectx"
)

// Provider hands out the database handle and owns its lifecycle | Provider 发放数据库句柄并拥有其生命周期.
type Provider interface {
	// DB returns the database handle | DB 返回数据库连接.
	DB(ctx context.Context) *DB

	// TX runs fn inside a database transaction | TX 在一个数据库事务中执行 fn.
	TX(ctx context.Context, fn func(ctx context.Context, tx *Tx) error) error

	// Close closes the database | Close 关闭数据库连接.
	io.Closer
}

// Modeler provides information of table.
// Objects support Insert/Update should implement this interface.
// Modeler 提供表信息，支持 Insert/Update 的对象需实现该接口.
type Modeler interface {
	// TableName return table name | TableName 返回表名.
	TableName() string
	// TableName return primary key column name | KeyName 返回主键列名.
	KeyName() string
}

// mapExecer unifies DB and TX | mapExecer 统一 DB 与 TX 的执行接口.
type mapExecer interface {
	DriverName() string
	GetMapper() *reflectx.Mapper
	Rebind(string) string
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// DB extends the original sqlx.DB | DB 扩展原生 sqlx.DB.
type DB struct {
	*sqlx.DB
}

// Tx extends the original sqlx.Tx | Tx 扩展原生 sqlx.Tx.
type Tx struct {
	*sqlx.Tx
}

// Options holds the database configuration | Options 保存数据库配置.
type Options struct {
	dsn string

	maxOpenConns    int
	maxIdleConns    int
	connMaxLifetime time.Duration
	connMaxIdleTime time.Duration
}

// Option customizes Options | Option 定制 Options.
type Option interface {
	apply(*Options)
}

// OptionFunc adapts a plain function to Option | OptionFunc 把普通函数适配为 Option.
type OptionFunc func(*Options)

// apply runs the adapted function | apply 执行被适配的函数.
func (f OptionFunc) apply(o *Options) {
	f(o)
}

// DefaultOptions returns an independent default configuration | DefaultOptions 返回一份独立的默认配置.
func DefaultOptions() *Options {
	return &Options{
		maxOpenConns:    25,
		maxIdleConns:    5,
		connMaxLifetime: 30 * time.Minute,
		connMaxIdleTime: 5 * time.Minute,
	}
}

// NewOptions 从 DefaultOptions 出发，按顺序应用 opts。
// NewOptions starts from DefaultOptions and applies opts in order.
func NewOptions(opts ...Option) *Options {
	o := DefaultOptions()
	o.Apply(opts...)
	return o
}

// Apply 按顺序原地应用 opts，不重置默认值、不做校验。
// Apply applies opts in place, in order; it neither resets defaults nor validates.
func (o *Options) Apply(opts ...Option) {
	for _, opt := range opts {
		opt.apply(o)
	}
}

// WithDSN sets the data source name | WithDSN 设置数据源名称（DSN）.
func WithDSN(dsn string) Option {
	return OptionFunc(func(o *Options) {
		o.dsn = dsn
	})
}

// WithMaxOpenConns sets the maximum number of open connections (default 25).
// A non-positive value means unlimited, as in database/sql.
// WithMaxOpenConns 设置最大打开连接数（默认 25）。非正值表示不限制，同 database/sql.
func WithMaxOpenConns(n int) Option {
	return OptionFunc(func(o *Options) {
		o.maxOpenConns = n
	})
}

// WithMaxIdleConns sets the maximum number of idle connections (default 5).
// A non-positive value disables idle connection retention, as in database/sql.
// WithMaxIdleConns 设置最大空闲连接数（默认 5）。非正值表示不保留空闲连接，同 database/sql.
func WithMaxIdleConns(n int) Option {
	return OptionFunc(func(o *Options) {
		o.maxIdleConns = n
	})
}

// WithConnMaxLifetime sets the maximum connection lifetime (default 30 minutes).
// A non-positive value disables expiration by age, as in database/sql.
// WithConnMaxLifetime 设置连接最大存活时间（默认 30 分钟）。非正值表示不按存活时间过期，同 database/sql.
func WithConnMaxLifetime(d time.Duration) Option {
	return OptionFunc(func(o *Options) {
		o.connMaxLifetime = d
	})
}

// WithConnMaxIdleTime sets the maximum idle duration (default 5 minutes).
// A non-positive value disables expiration by idle time, as in database/sql.
// WithConnMaxIdleTime 设置连接最大空闲时间（默认 5 分钟）。非正值表示不按空闲时间过期，同 database/sql.
func WithConnMaxIdleTime(d time.Duration) Option {
	return OptionFunc(func(o *Options) {
		o.connMaxIdleTime = d
	})
}
