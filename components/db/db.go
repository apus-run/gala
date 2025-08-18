package db

import (
	"context"
	"database/sql"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/utils"
	"gorm.io/plugin/dbresolver"
)

var _ Provider = (*provider)(nil)

// provider 包装 gorm.db 并强制提供 ctx 以串联 trace
type provider struct {
	db *gorm.DB
}

// NewDB 创建一个 db 实例
func NewDB(dialer gorm.Dialector, opts ...gorm.Option) (Provider, error) {
	db, err := gorm.Open(dialer, opts...)
	if err != nil {
		return nil, err
	}
	return &provider{db: db}, nil
}

// NewDBFromConfig 从配置创建一个 db 实例
func NewDBFromConfig(dsn string, opts ...gorm.Option) (Provider, error) {
	if !utils.Contains(mysql.UpdateClauses, "RETURNING") {
		mysql.UpdateClauses = append(mysql.UpdateClauses, "RETURNING")
	}
	opts = append(opts, &gorm.Config{
		TranslateError: true,
	})

	db, err := gorm.Open(mysql.Open(dsn), opts...)
	if err != nil {
		return nil, err
	}

	return &provider{db: db}, nil
}

func Close(db *gorm.DB) error {
	if db == nil {
		return nil
	}

	sqlDB, err := db.DB()
	if err != nil {
		return err
	}

	checkInUse(sqlDB, time.Second*5)

	return sqlDB.Close()
}

func checkInUse(sqlDB *sql.DB, duration time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()
	for {
		select {
		case <-time.After(time.Millisecond * 250):
			if v := sqlDB.Stats().InUse; v == 0 {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

func (p *provider) DB(ctx context.Context, opts ...Option) *gorm.DB {
	session := p.db

	opt := &option{}
	for _, fn := range opts {
		fn(opt)
	}
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
	if opt.forUpdate {
		session = session.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	return session.WithContext(ctx)
}

func (p *provider) TX(ctx context.Context, fn func(tx *gorm.DB) error, opts ...Option) error {
	session := p.DB(ctx, opts...)
	return session.Transaction(fn)
}
