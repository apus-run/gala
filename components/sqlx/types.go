package sqlx

import (
	"context"
	"database/sql"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/jmoiron/sqlx/reflectx"
)

type Provider interface {
	// DB 返回数据库连接
	DB(ctx context.Context) *DB

	// TX 执行一个函数在一个数据库事务中
	TX(ctx context.Context, fn func(ctx context.Context, tx *Tx) error) error

	// Close 关闭数据库连接
	Close() error
}

// Modeler provides information of table.
// Objects support Insert/Update should implement this interface.
type Modeler interface {
	// TableName return table name.
	TableName() string
	// TableName return primary key column name.
	KeyName() string
}

// mapExecer unifies DB and TX
type mapExecer interface {
	DriverName() string
	GetMapper() *reflectx.Mapper
	Rebind(string) string
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// DB extends the original sqlx.DB
type DB struct {
	*sqlx.DB
}

// Tx extends the original sqlx.Tx
type Tx struct {
	*sqlx.Tx
}

type Options struct {
	dsn string

	maxOpenConns    int
	maxIdleConns    int
	connMaxLifetime time.Duration
	connMaxIdleTime time.Duration
}

type Option func(*Options)

func Apply(opts ...Option) *Options {
	o := &Options{
		maxOpenConns:    25,
		maxIdleConns:    5,
		connMaxLifetime: 30 * time.Minute,
		connMaxIdleTime: 5 * time.Minute,
	}
	for _, fn := range opts {
		fn(o)
	}
	return o
}

func WithDSN(dsn string) Option {
	return func(o *Options) {
		o.dsn = dsn
	}
}

func WithMaxOpenConns(n int) Option {
	return func(o *Options) {
		o.maxOpenConns = n
	}
}

func WithMaxIdleConns(n int) Option {
	return func(o *Options) {
		o.maxIdleConns = n
	}
}

func WithConnMaxLifetime(d time.Duration) Option {
	return func(o *Options) {
		o.connMaxLifetime = d
	}
}

func WithConnMaxIdleTime(d time.Duration) Option {
	return func(o *Options) {
		o.connMaxIdleTime = d
	}
}
