package gorm_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/apus-run/gala/components/dlock"
	dlockgorm "github.com/apus-run/gala/components/dlock/gorm"
)

func openStore(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "lock.db")), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	return db
}

func TestMigrationFailures(t *testing.T) {
	for _, column := range []string{"migrate", "version", "token"} {
		t.Run(column, func(t *testing.T) {
			db := openStore(t)
			if column == "migrate" {
				pool, err := db.DB()
				if err != nil {
					t.Fatal(err)
				}
				if err := pool.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				err := db.Callback().Update().Before("gorm:update").Register("test:deny-column", func(tx *gorm.DB) {
					if values, ok := tx.Statement.Dest.(map[string]any); ok {
						if _, found := values[column]; found {
							tx.AddError(errors.New("write denied"))
						}
					}
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := dlockgorm.Init(t.Context(), db); err == nil || !strings.Contains(err.Error(), column) {
				t.Fatalf("Init() = %v", err)
			}
		})
	}
}

func TestStoreErrorsAndLostUpdates(t *testing.T) {
	storeErr := errors.New("store unavailable")
	for _, path := range []string{"insert", "read", "extend", "takeover", "refresh", "release", "lost extend", "lost takeover"} {
		t.Run(path, func(t *testing.T) {
			db := openStore(t)
			ctx := t.Context()
			f := dlockgorm.NewClient(db)
			if err := f.Init(ctx); err != nil {
				t.Fatal(err)
			}
			if l, err := f.NewLock(ctx, dlock.WithLockName("")); l != nil || err == nil {
				t.Fatalf("invalid lock %v %v", l, err)
			}
			l, err := f.NewLock(ctx)
			if err != nil {
				t.Fatal(err)
			}
			switch path {
			case "insert":
			case "takeover", "lost takeover":
				if err := db.Create(&dlockgorm.GORMLock{Name: dlock.DefaultLockName, Token: "previous", ExpiredAt: time.Now().Add(-time.Minute)}).Error; err != nil {
					t.Fatal(err)
				}
			default:
				if err := l.Lock(ctx); err != nil {
					t.Fatal(err)
				}
			}
			operation := l.Lock
			fail := func(tx *gorm.DB) { tx.AddError(storeErr) }
			want := storeErr
			switch path {
			case "insert":
				err = db.Callback().Create().Before("gorm:create").Register("test:error", fail)
			case "read":
				err = db.Callback().Query().Before("gorm:query").Register("test:error", fail)
			case "extend", "takeover", "refresh":
				if path == "refresh" {
					operation = l.Refresh
				}
				err = db.Callback().Update().Before("gorm:update").Register("test:error", fail)
			case "release":
				operation = l.Unlock
				err = db.Callback().Delete().Before("gorm:delete").Register("test:error", fail)
			case "lost extend", "lost takeover":
				// Simulate a newer version or owner winning before the conditional update.
				err = db.Callback().Update().Before("gorm:update").Register("test:lost-update", func(tx *gorm.DB) {
					tx.Statement.AddClause(clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "1 = 0"}}})
				})
				want = dlock.ErrLocked
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := operation(ctx); !errors.Is(err, want) {
				t.Fatalf("got %v, want %v", err, want)
			}
		})
	}
}
