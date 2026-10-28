package db

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/plugin/dbresolver"
)

var _ Provider = (*provider)(nil)

// txKey 是 TX 把事务放进 ctx 时使用的键。
// 绑定 provider 实例：同一进程连多个库时，各自只取自己的事务。
type txKey struct{ p *provider }

// provider 包装 gorm.db 并强制提供 ctx 以串联 trace
type provider struct {
	db *gorm.DB
}

// NewDB 创建一个 db 实例，驱动由调用方选择，如 NewDB(mysql.Open(dsn))。
//
// 总是开启 TranslateError：驱动错误统一翻译为 gorm.ErrDuplicatedKey、
// gorm.ErrForeignKeyViolated 等，调用方用 errors.Is 判断，与驱动无关。
func NewDB(dialer gorm.Dialector, opts ...gorm.Option) (Provider, error) {
	db, err := gorm.Open(dialer, withTranslateError(opts)...)
	if err != nil {
		return nil, err
	}
	return &provider{db: db}, nil
}

func Unwrap(db Provider) (*gorm.DB, bool) {
	if p, ok := db.(*provider); ok && p.db != nil {
		return p.db, true
	}
	return nil, false
}

// withTranslateError 在调用方已有的 *gorm.Config 上开启 TranslateError。
// 不能再追加一个 Config：gorm.Open 按整体赋值应用 Config，后者会覆盖前者。
func withTranslateError(opts []gorm.Option) []gorm.Option {
	for _, o := range opts {
		if c, ok := o.(*gorm.Config); ok {
			c.TranslateError = true
			return opts
		}
	}
	return append(opts, &gorm.Config{TranslateError: true})
}

func (p *provider) Close() error {
	if p.db == nil {
		return nil
	}

	sqlDB, err := p.db.DB()
	if err != nil {
		return err
	}

	// sql.DB.Close 会等待已开始的查询结束。
	return sqlDB.Close()
}

func (p *provider) DB(ctx context.Context, opts ...Option) *gorm.DB {
	session := p.db
	if tx, ok := ctx.Value(txKey{p}).(*gorm.DB); ok {
		session = tx
	}

	opt := Apply(opts...)

	if opt.tx != nil {
		session = opt.tx
	}
	if opt.debug {
		session = session.Debug()
	}
	if opt.withMaster {
		session = session.Clauses(dbresolver.Write)
	}
	if opt.withDeleted {
		session = session.Unscoped()
	}
	if opt.selectForUpdate {
		session = session.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	return session.WithContext(ctx)
}

func (p *provider) TX(ctx context.Context, fn func(ctx context.Context) error, opts ...Option) error {
	// ctx 里已有事务时，gorm 会以 savepoint 嵌套。
	return p.DB(ctx, opts...).Transaction(func(tx *gorm.DB) error {
		return fn(context.WithValue(ctx, txKey{p}, tx))
	})
}
