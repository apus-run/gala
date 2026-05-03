package db

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/utils"
	"gorm.io/plugin/dbresolver"
)

var (
	_ Provider = (*provider)(nil)

	once sync.Once
)

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

func Unwrap(db Provider) (*gorm.DB, bool) {
	if p, ok := db.(*provider); ok && p.db != nil {
		return p.db, true
	}
	return nil, false
}

// NewMySQLFromConfig 从 MySQL DSN 创建一个 db 实例
func NewMySQLFromConfig(dsn string, opts ...gorm.Option) (Provider, error) {
	once.Do(func() {
		if !utils.Contains(mysql.UpdateClauses, "RETURNING") {
			mysql.UpdateClauses = append(mysql.UpdateClauses, "RETURNING")
		}
	})
	opts = append(opts, &gorm.Config{
		TranslateError: true,
	})

	db, err := gorm.Open(mysql.Open(dsn), opts...)
	if err != nil {
		return nil, err
	}

	return &provider{db: db}, nil
}

func (p *provider) Close() error {
	if p.db == nil {
		return nil
	}

	sqlDB, err := p.db.DB()
	if err != nil {
		return err
	}

	waitInUse(sqlDB, 5*time.Second)

	return sqlDB.Close()
}

func waitInUse(sqlDB *sql.DB, duration time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if sqlDB.Stats().InUse == 0 {
				return
			}
		case <-ctx.Done():
			slog.Warn("close db: connections still in use after timeout",
				"in_use", sqlDB.Stats().InUse)
			return
		}
	}
}

func (p *provider) DB(ctx context.Context, opts ...Option) *gorm.DB {
	session := p.db

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

func (p *provider) TX(ctx context.Context, fn func(tx *gorm.DB) error, opts ...Option) error {
	session := p.DB(ctx, opts...)
	return session.Transaction(fn)
}
