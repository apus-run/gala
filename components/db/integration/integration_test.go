// Package integration 在真实 MySQL / PostgreSQL 上验证 db 组件的行为。
//
// 启动数据库见 docker-compose.yml。未设置 DSN 环境变量时对应用例整体 skip，
// 所以没有 docker 的环境里 go test 仍然是绿的。
package integration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/apus-run/gala/components/db"
	"github.com/apus-run/gala/components/db/page"
	"github.com/apus-run/gala/components/db/repo"
	"github.com/apus-run/gala/components/db/store"
	"github.com/apus-run/gala/components/db/where"
)

type backend struct {
	name   string
	envDSN string
	open   func(dsn string) gorm.Dialector
}

var backends = []backend{
	{"mysql", "GALA_DB_MYSQL_DSN", func(dsn string) gorm.Dialector { return mysql.Open(dsn) }},
	{"postgres", "GALA_DB_POSTGRES_DSN", func(dsn string) gorm.Dialector { return postgres.Open(dsn) }},
}

// Account 同时覆盖唯一键、外键、软删、乐观更新所需的列类型。
type Account struct {
	ID        uint   `gorm:"primaryKey"`
	Name      string `gorm:"size:64;uniqueIndex"`
	Balance   int
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

type Entry struct {
	ID        uint `gorm:"primaryKey"`
	AccountID uint `gorm:"index"`
	Amount    int
	Account   Account `gorm:"foreignKey:AccountID"`
}

// forEach 对每个已配置的数据库各跑一遍子测试，每个子测试拿到干净的表。
func forEach(t *testing.T, fn func(t *testing.T, p db.Provider, b backend)) {
	t.Helper()
	ran := 0
	for _, b := range backends {
		dsn := os.Getenv(b.envDSN)
		if dsn == "" {
			t.Run(b.name, func(t *testing.T) { t.Skipf("set %s to run", b.envDSN) })
			continue
		}
		ran++
		t.Run(b.name, func(t *testing.T) {
			p, err := db.NewDB(b.open(dsn))
			require.NoError(t, err)
			t.Cleanup(func() { _ = p.Close() })

			g := p.DB(context.Background())
			require.NoError(t, g.Migrator().DropTable(&Entry{}, &Account{}))
			require.NoError(t, g.AutoMigrate(&Account{}, &Entry{}))
			fn(t, p, b)
		})
	}
	_ = ran
}

func names(t *testing.T, p db.Provider) []string {
	t.Helper()
	var out []string
	require.NoError(t, p.DB(context.Background()).Model(&Account{}).Order("name").Pluck("name", &out).Error)
	return out
}

// 错误翻译：唯一键、外键冲突在两种数据库上都变成 gorm 统一错误。
func TestTranslateError(t *testing.T) {
	forEach(t, func(t *testing.T, p db.Provider, b backend) {
		ctx := context.Background()
		require.NoError(t, p.DB(ctx).Create(&Account{Name: "dup"}).Error)

		err := p.DB(ctx).Create(&Account{Name: "dup"}).Error
		require.ErrorIs(t, err, gorm.ErrDuplicatedKey)

		err = p.DB(ctx).Create(&Entry{AccountID: 99999, Amount: 1}).Error
		require.ErrorIs(t, err, gorm.ErrForeignKeyViolated)

		var acc Account
		require.NoError(t, p.DB(ctx).First(&acc, "name = ?", "dup").Error)
		require.NoError(t, p.DB(ctx).Create(&Entry{AccountID: acc.ID, Amount: 1}).Error)
		err = p.DB(ctx).Unscoped().Delete(&acc).Error
		require.ErrorIs(t, err, gorm.ErrForeignKeyViolated, "删除被引用的行应触发外键错误")
	})
}

// 事务：回滚、提交、嵌套 savepoint（内层失败只回滚内层）、多层嵌套。
func TestTXRollbackCommitSavepoint(t *testing.T) {
	forEach(t, func(t *testing.T, p db.Provider, b backend) {
		ctx := context.Background()
		boom := errors.New("boom")

		err := p.TX(ctx, func(ctx context.Context) error {
			require.NoError(t, p.DB(ctx).Create(&Account{Name: "rolled-back"}).Error)
			return boom
		})
		require.ErrorIs(t, err, boom)
		require.Empty(t, names(t, p))

		require.NoError(t, p.TX(ctx, func(ctx context.Context) error {
			return p.DB(ctx).Create(&Account{Name: "committed"}).Error
		}))
		require.Equal(t, []string{"committed"}, names(t, p))

		require.NoError(t, p.TX(ctx, func(ctx context.Context) error {
			require.NoError(t, p.DB(ctx).Create(&Account{Name: "outer"}).Error)

			inner := p.TX(ctx, func(ctx context.Context) error {
				require.NoError(t, p.DB(ctx).Create(&Account{Name: "inner"}).Error)
				return boom
			})
			require.ErrorIs(t, inner, boom)

			return p.TX(ctx, func(ctx context.Context) error { // 第二个同级 savepoint 仍可用
				return p.DB(ctx).Create(&Account{Name: "inner2"}).Error
			})
		}))
		require.Equal(t, []string{"committed", "inner2", "outer"}, names(t, p))

		// 三层嵌套：最内层失败，只回滚最内层；中间层的失败吞掉后外层照常提交。
		require.NoError(t, p.TX(ctx, func(ctx context.Context) error {
			_ = p.TX(ctx, func(ctx context.Context) error {
				require.NoError(t, p.DB(ctx).Create(&Account{Name: "mid"}).Error)
				_ = p.TX(ctx, func(ctx context.Context) error {
					require.NoError(t, p.DB(ctx).Create(&Account{Name: "deep"}).Error)
					return boom
				})
				return nil
			})
			return nil
		}))
		require.Contains(t, names(t, p), "mid")
		require.NotContains(t, names(t, p), "deep")
	})
}

// 唯一键冲突发生在事务内。Postgres 里出错的语句会让整个事务 aborted，
// 只有回滚到 savepoint 才能继续；MySQL 则不受影响。TX 嵌套时必须在两边都能恢复。
func TestTXConflictInsideNestedSavepoint(t *testing.T) {
	forEach(t, func(t *testing.T, p db.Provider, b backend) {
		ctx := context.Background()
		require.NoError(t, p.DB(ctx).Create(&Account{Name: "taken"}).Error)

		err := p.TX(ctx, func(ctx context.Context) error {
			require.NoError(t, p.DB(ctx).Create(&Account{Name: "before"}).Error)

			conflict := p.TX(ctx, func(ctx context.Context) error {
				return p.DB(ctx).Create(&Account{Name: "taken"}).Error
			})
			require.ErrorIs(t, conflict, gorm.ErrDuplicatedKey)

			return p.DB(ctx).Create(&Account{Name: "after"}).Error // 外层事务必须仍然可用
		})
		require.NoError(t, err)
		require.Equal(t, []string{"after", "before", "taken"}, names(t, p))
	})
}

// panic 会回滚事务并继续向外抛。
func TestTXPanicRollsBack(t *testing.T) {
	forEach(t, func(t *testing.T, p db.Provider, b backend) {
		ctx := context.Background()
		require.Panics(t, func() {
			_ = p.TX(ctx, func(ctx context.Context) error {
				_ = p.DB(ctx).Create(&Account{Name: "panicked"}).Error
				panic("oops")
			})
		})
		require.Empty(t, names(t, p))

		// 连接没有泄漏：回滚后还能正常使用。
		require.NoError(t, p.DB(ctx).Create(&Account{Name: "ok"}).Error)
	})
}

// 行锁：两个事务对同一行先 SELECT ... FOR UPDATE 再读改写，不会丢更新。
func TestSelectForUpdateSerializes(t *testing.T) {
	forEach(t, func(t *testing.T, p db.Provider, b backend) {
		ctx := context.Background()
		acc := &Account{Name: "counter", Balance: 0}
		require.NoError(t, p.DB(ctx).Create(acc).Error)

		const workers, rounds = 8, 10
		var wg sync.WaitGroup
		var failed atomic.Int32
		for range workers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for range rounds {
					err := p.TX(ctx, func(ctx context.Context) error {
						var cur Account
						if err := p.DB(ctx, db.WithSelectForUpdate()).First(&cur, acc.ID).Error; err != nil {
							return err
						}
						return p.DB(ctx).Model(&cur).Update("balance", cur.Balance+1).Error
					})
					if err != nil {
						failed.Add(1)
					}
				}
			}()
		}
		wg.Wait()

		require.Zero(t, failed.Load())
		var got Account
		require.NoError(t, p.DB(ctx).First(&got, acc.ID).Error)
		require.Equal(t, workers*rounds, got.Balance, "有 FOR UPDATE 就不会丢更新")
	})
}

// 没有锁时，同样的读改写会丢更新：证明上面的断言不是巧合通过。
func TestWithoutLockLosesUpdates(t *testing.T) {
	forEach(t, func(t *testing.T, p db.Provider, b backend) {
		ctx := context.Background()
		acc := &Account{Name: "racy"}
		require.NoError(t, p.DB(ctx).Create(acc).Error)

		const workers, rounds = 8, 10
		var wg sync.WaitGroup
		for range workers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for range rounds {
					_ = p.TX(ctx, func(ctx context.Context) error {
						var cur Account
						if err := p.DB(ctx).First(&cur, acc.ID).Error; err != nil {
							return err
						}
						time.Sleep(time.Millisecond)
						return p.DB(ctx).Model(&cur).Update("balance", cur.Balance+1).Error
					})
				}
			}()
		}
		wg.Wait()

		var got Account
		require.NoError(t, p.DB(ctx).First(&got, acc.ID).Error)
		require.Less(t, got.Balance, workers*rounds, "无锁读改写应该丢更新")
	})
}

// 软删：默认不可见，WithDeleted 可见；唯一键在软删后仍被占用。
func TestSoftDeleteAndWithDeleted(t *testing.T) {
	forEach(t, func(t *testing.T, p db.Provider, b backend) {
		ctx := context.Background()
		acc := &Account{Name: "ghost"}
		require.NoError(t, p.DB(ctx).Create(acc).Error)
		require.NoError(t, p.DB(ctx).Delete(acc).Error)

		var visible, all int64
		require.NoError(t, p.DB(ctx).Model(&Account{}).Count(&visible).Error)
		require.NoError(t, p.DB(ctx, db.WithDeleted()).Model(&Account{}).Count(&all).Error)
		require.EqualValues(t, 0, visible)
		require.EqualValues(t, 1, all)

		err := p.DB(ctx).Create(&Account{Name: "ghost"}).Error
		require.ErrorIs(t, err, gorm.ErrDuplicatedKey)
	})
}

// store + where + page 在真实数据库上端到端：过滤、白名单排序、分页、LIKE 转义、事务。
func TestStoreListEndToEnd(t *testing.T) {
	forEach(t, func(t *testing.T, p db.Provider, b backend) {
		ctx := context.Background()
		s := store.NewStore[Account](p)
		for i := range 25 {
			require.NoError(t, s.Create(ctx, &Account{Name: fmt.Sprintf("user-%02d", i), Balance: i}))
		}
		require.NoError(t, s.Create(ctx, &Account{Name: "100%_sure", Balance: 1000}))

		sortable := where.Sortable{"name": "name", "balance": "balance"}
		cols, err := sortable.Parse("-balance")
		require.NoError(t, err)

		params := page.Params{Page: 2, Size: 10}
		total, rows, err := s.List(ctx, where.P(params.Page, params.Size).S(cols...))
		require.NoError(t, err)
		require.EqualValues(t, 26, total)
		require.Len(t, rows, 10)
		require.Equal(t, 15, rows[0].Balance) // 第 1 页：1000, 24..16；第 2 页从 15 开始
		require.Equal(t, 6, rows[9].Balance)

		meta := page.NewMeta(params, total)
		require.Equal(t, 3, meta.TotalPages)
		require.True(t, meta.HasNext)
		require.True(t, meta.HasPrev)

		_, err = sortable.Parse("name;drop table accounts")
		require.ErrorIs(t, err, where.ErrInvalidSort)

		n, err := s.Count(ctx, where.F("balance", 7))
		require.NoError(t, err)
		require.EqualValues(t, 1, n)

		_, err = s.Get(ctx, where.F("name", "nobody"))
		require.ErrorIs(t, err, gorm.ErrRecordNotFound)

		err = p.TX(ctx, func(ctx context.Context) error {
			require.NoError(t, s.Create(ctx, &Account{Name: "in-tx"}))
			return errors.New("abort")
		})
		require.Error(t, err)
		_, err = s.Get(ctx, where.F("name", "in-tx"))
		require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	})
}

// 两个 Provider（两个连接池）的事务互不串用。MySQL 与 Postgres 各自用同一个库开两个 Provider。
func TestTXIsolatedPerProvider(t *testing.T) {
	forEach(t, func(t *testing.T, a db.Provider, b backend) {
		ctx := context.Background()
		other, err := db.NewDB(b.open(os.Getenv(b.envDSN)))
		require.NoError(t, err)
		t.Cleanup(func() { _ = other.Close() })

		err = a.TX(ctx, func(ctx context.Context) error {
			require.NoError(t, a.DB(ctx).Create(&Account{Name: "via-a"}).Error)
			require.NoError(t, other.DB(ctx).Create(&Account{Name: "via-other"}).Error) // 自己的连接，自动提交
			return errors.New("rollback a")
		})
		require.Error(t, err)
		require.Equal(t, []string{"via-other"}, names(t, a))
	})
}

// 新建 Provider 时调用方传入的 Config 不被覆盖，同时错误翻译仍然开启。
func TestNewDBKeepsCallerConfig(t *testing.T) {
	forEach(t, func(t *testing.T, _ db.Provider, b backend) {
		cfg := &gorm.Config{SkipDefaultTransaction: true, NamingStrategy: nil}
		p, err := db.NewDB(b.open(os.Getenv(b.envDSN)), cfg)
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		g, ok := db.Unwrap(p)
		require.True(t, ok)
		require.True(t, g.Config.SkipDefaultTransaction, "调用方的 Config 被覆盖了")
		require.True(t, g.Config.TranslateError)
	})
}

// Locking 子句生成的 SQL 与「更新为相同值」在两种库上都不报错。
func TestRawSQLClauses(t *testing.T) {
	forEach(t, func(t *testing.T, p db.Provider, b backend) {
		ctx := context.Background()
		require.NoError(t, p.DB(ctx).Create(&Account{Name: "like-me", Balance: 5}).Error)

		// Locking 子句生成的 SQL 在两种库上都合法（只在事务内有意义）。
		require.NoError(t, p.TX(ctx, func(ctx context.Context) error {
			var acc Account
			return p.DB(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&acc, "name = ?", "like-me").Error
		}))

		// 更新为相同的值：MySQL 默认 RowsAffected=0，Postgres 为 1。这里只断言不报错。
		require.NoError(t, p.DB(ctx).Model(&Account{}).Where("name = ?", "like-me").Update("balance", 5).Error)

		var n int64
		require.NoError(t, p.DB(ctx).Model(&Account{}).Where("name LIKE ?", "%"+strings.ToLower("ME")).Count(&n).Error)
		require.EqualValues(t, 1, n)
	})
}

// repo.Update 用 RowsAffected==0 判断「记录不存在」。MySQL 默认返回「实际改变的行数」，
// 更新成相同的值时是 0；Postgres 返回「匹配的行数」。同一行为在两种库上必须一致。
func TestRepoUpdateSameValue(t *testing.T) {
	forEach(t, func(t *testing.T, p db.Provider, b backend) {
		ctx := context.Background()
		r := repo.NewGormRepository(p.DB(ctx), Account{}, "accounts")
		acc := &Account{Name: "same", Balance: 5}
		require.NoError(t, r.Create(ctx, acc))

		err := r.Update(ctx, acc.ID, &Account{Balance: 5})
		require.NoError(t, err, "记录存在，更新成相同的值不应报 not found")

		err = r.Update(ctx, 987654, &Account{Balance: 1})
		require.ErrorIs(t, err, repo.ErrRecordNotFound)
	})
}

// UpdateBatch 在 WithTransaction 的事务里嵌套 savepoint；批内失败整体回滚。
func TestRepoUpdateBatchTransactional(t *testing.T) {
	forEach(t, func(t *testing.T, p db.Provider, b backend) {
		ctx := context.Background()
		r := repo.NewGormRepository(p.DB(ctx), Account{}, "accounts")
		a, c := &Account{Name: "a", Balance: 1}, &Account{Name: "c", Balance: 3}
		require.NoError(t, r.Create(ctx, a))
		require.NoError(t, r.Create(ctx, c))

		// 第二条把 name 改成已存在的 "a"，唯一键冲突，整批回滚。
		err := r.UpdateBatch(ctx, []any{a.ID, c.ID}, []Account{{Balance: 10}, {Name: "a"}})
		require.ErrorIs(t, err, gorm.ErrDuplicatedKey)
		var got Account
		require.NoError(t, p.DB(ctx).First(&got, a.ID).Error)
		require.Equal(t, 1, got.Balance, "批内失败，第一条也必须回滚")

		// 在外层事务里调用：内层失败只回滚内层，外层之后仍可用。
		require.NoError(t, p.TX(ctx, func(ctx context.Context) error {
			txRepo := r.WithTransaction(p.DB(ctx))
			bad := txRepo.UpdateBatch(ctx, []any{a.ID, c.ID}, []Account{{Balance: 10}, {Name: "a"}})
			require.ErrorIs(t, bad, gorm.ErrDuplicatedKey)
			return txRepo.UpdateBatch(ctx, []any{a.ID}, []Account{{Balance: 20}})
		}))
		require.NoError(t, p.DB(ctx).First(&got, a.ID).Error)
		require.Equal(t, 20, got.Balance)
	})
}

// 分页、计数、聚合在两种库上结果一致。
func TestRepoPaginateAndAtomic(t *testing.T) {
	forEach(t, func(t *testing.T, p db.Provider, b backend) {
		ctx := context.Background()
		r := repo.NewGormRepository(p.DB(ctx), Account{}, "accounts")
		for i := range 25 {
			require.NoError(t, r.Create(ctx, &Account{Name: fmt.Sprintf("u%02d", i), Balance: i}))
		}

		meta, rows, err := r.OrderBy("id", "ASC").Paginate(3, 10).Execute(ctx)
		require.NoError(t, err)
		require.EqualValues(t, 25, meta.Total)
		require.Len(t, rows, 5)
		require.False(t, meta.HasNext)

		var first Account
		require.NoError(t, p.DB(ctx).Order("id").First(&first).Error)
		require.NoError(t, r.Increment(ctx, first.ID, "balance", 100))
		require.NoError(t, r.Decrement(ctx, first.ID, "balance", 40))
		got, err := r.FindByID(ctx, first.ID)
		require.NoError(t, err)
		require.Equal(t, 60, got.Balance)

		n, err := r.WhereBetween("balance", 10, 20).Count(ctx)
		require.NoError(t, err)
		require.EqualValues(t, 11, n)
	})
}

// ---- 审核问题的修复在真实数据库上的验证 ----

// 字段名注入：合法列名在两种库上都被正确加引号（MySQL 反引号，Postgres 双引号），
// 保留字列名也能用；恶意字段名只会被当成不存在的列。
type Ticket struct {
	ID    uint `gorm:"primaryKey"`
	Title string
	Order int `gorm:"column:order"` // SQL 保留字
	Note  *string
}

func ticketRepo(t *testing.T, p db.Provider) repo.Repository[Ticket] {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, p.DB(ctx).Migrator().DropTable(&Ticket{}))
	require.NoError(t, p.DB(ctx).AutoMigrate(&Ticket{}))
	note := "n"
	require.NoError(t, p.DB(ctx).Create(&[]Ticket{
		{Title: "t1", Order: 1, Note: &note}, {Title: "t2", Order: 2}, {Title: "t3", Order: 3, Note: &note},
	}).Error)
	return repo.NewGormRepository(p.DB(ctx), Ticket{}, "tickets")
}

func TestRepoFieldNamesAreQuoted(t *testing.T) {
	forEach(t, func(t *testing.T, p db.Provider, b backend) {
		ctx := context.Background()
		r := ticketRepo(t, p)

		rows, err := r.Where("order", 2).Find(ctx)
		require.NoError(t, err)
		require.Len(t, rows, 1)

		rows, err = r.WhereIn("order", []any{1, 3}).OrderByDesc("order").Find(ctx)
		require.NoError(t, err)
		require.Equal(t, []int{3, 1}, []int{rows[0].Order, rows[1].Order})

		rows, err = r.WhereBetween("order", 2, 3).OrderByAsc("order").Find(ctx)
		require.NoError(t, err)
		require.Len(t, rows, 2)

		rows, err = r.WhereNull("note").Find(ctx)
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.Equal(t, "t2", rows[0].Title)

		rows, err = r.WhereNotNull("note").OrderByAsc("id").Find(ctx)
		require.NoError(t, err)
		require.Len(t, rows, 2)

		require.NoError(t, r.Increment(ctx, 1, "order", 10))
		got, err := r.FindByID(ctx, 1)
		require.NoError(t, err)
		require.Equal(t, 11, got.Order)
		require.NoError(t, r.Decrement(ctx, 1, "order", 4))
		got, _ = r.FindByID(ctx, 1)
		require.Equal(t, 7, got.Order)

		vals, err := r.PluckInt(ctx, "order")
		require.NoError(t, err)
		require.Len(t, vals, 3)

		// 恶意字段名，包括试图闭合引号的写法：MySQL 会把反引号双写，Postgres 直接拒绝。
		for _, f := range []string{
			"title; DROP TABLE tickets",
			"note IS NULL OR 1=1 OR note",
			"order = 999, title",
			"title FROM tickets --",
			"title) OR 1=1 --",
			"title` OR 1=1 OR `title",
			`title" OR 1=1 OR "title`,
		} {
			_, err = r.WhereNull(f).Find(ctx)
			require.Error(t, err, f)
			require.Error(t, r.Increment(ctx, 1, f, 1), f)
			_, err = r.Pluck(ctx, f)
			require.Error(t, err, f)
			_, err = r.Where("title", "t1").GroupBy(f).Find(ctx)
			require.Error(t, err, f)
		}
		n, err := r.Count(ctx)
		require.NoError(t, err)
		require.EqualValues(t, 3, n)
	})
}

// 主键参数永远不是原始 SQL：两种库对「字符串 vs 整数主键」的反应不同
// （MySQL 报截断错误，Postgres 报类型错误），共同点是：不成功、不命中、不删数据。
func TestRepoPrimaryKeyInjection(t *testing.T) {
	forEach(t, func(t *testing.T, p db.Provider, b backend) {
		ctx := context.Background()
		r := ticketRepo(t, p)

		_, err := r.FindByID(ctx, "0 OR 1=1")
		require.Error(t, err)
		require.Error(t, r.Delete(ctx, "0 OR 1=1"))
		n, err := r.Count(ctx)
		require.NoError(t, err)
		require.EqualValues(t, 3, n)

		got, err := r.FindByID(ctx, 2)
		require.NoError(t, err)
		require.Equal(t, "t2", got.Title)
		require.NoError(t, r.Delete(ctx, 2))
	})
}

func TestRepoExists(t *testing.T) {
	forEach(t, func(t *testing.T, p db.Provider, b backend) {
		ctx := context.Background()
		r := ticketRepo(t, p)

		ok, err := r.Exists(ctx)
		require.NoError(t, err)
		require.True(t, ok)
		ok, err = r.Where("title", "t2").Exists(ctx)
		require.NoError(t, err)
		require.True(t, ok)
		ok, err = r.Where("title", "nobody").Exists(ctx)
		require.NoError(t, err)
		require.False(t, ok)
		ok, err = r.WhereNull("note").Exists(ctx)
		require.NoError(t, err)
		require.True(t, ok)
		dne, err := r.Where("title", "nobody").DoesNotExist(ctx)
		require.NoError(t, err)
		require.True(t, dne)
	})
}

// GroupBy 多字段与无字段在两种库上都能执行（Postgres 对 GROUP BY 更严格）。
func TestRepoGroupBy(t *testing.T) {
	forEach(t, func(t *testing.T, p db.Provider, b backend) {
		ctx := context.Background()
		r := ticketRepo(t, p)

		type agg struct {
			Title string
			N     int
		}
		var out []agg
		require.NoError(t, p.DB(ctx).Model(&Ticket{}).Select("title", "COUNT(*) AS n").
			Clauses(clause.GroupBy{Columns: []clause.Column{{Name: "title"}}}).Having("COUNT(*) >= ?", 1).Order("title").Scan(&out).Error)
		require.Len(t, out, 3)

		rows, err := r.Where("title", "t1").GroupBy().Find(ctx)
		require.NoError(t, err)
		require.Len(t, rows, 1)
		rows, err = r.WhereNotNull("title").Select("title").GroupBy("title").Find(ctx)
		require.NoError(t, err)
		require.Len(t, rows, 3)
	})
}

// store.Count 忽略分页，store.List 默认只取一页；两种库上语义一致。
func TestStoreCountAndListDefaults(t *testing.T) {
	forEach(t, func(t *testing.T, p db.Provider, b backend) {
		ctx := context.Background()
		s := store.NewStore[Account](p)
		for i := range page.DefaultSize + 5 {
			require.NoError(t, s.Create(ctx, &Account{Name: fmt.Sprintf("acc-%03d", i)}))
		}
		total := int64(page.DefaultSize + 5)

		for _, opts := range []*where.Options{where.P(2, 10), where.L(3), where.O(40)} {
			n, err := s.Count(ctx, opts)
			require.NoError(t, err)
			require.Equal(t, total, n)
		}

		count, rows, err := s.List(ctx, nil)
		require.NoError(t, err)
		require.Equal(t, total, count)
		require.Len(t, rows, page.DefaultSize)

		// 分页偏移超出末页：两种库都返回空页而不是报错。
		count, rows, err = s.List(ctx, where.P(99, 10))
		require.NoError(t, err)
		require.Equal(t, total, count)
		require.Empty(t, rows)
	})
}

// 租户过滤：正确使用时只返回本租户的行；F 参数不成对时既不 panic、也不静默丢条件，
// 而是返回错误且不执行。
func TestTenantIsolation(t *testing.T) {
	forEach(t, func(t *testing.T, p db.Provider, b backend) {
		ctx := context.Background()
		type k struct{}
		where.RegisterTenant("name", func(ctx context.Context) string { return ctx.Value(k{}).(string) })
		t.Cleanup(func() { where.RegisterTenant("", nil) })

		s := store.NewStore[Account](p)
		require.NoError(t, s.Create(ctx, &Account{Name: "tenant-a", Balance: 1}))
		require.NoError(t, s.Create(ctx, &Account{Name: "tenant-b", Balance: 2}))

		ctxA := context.WithValue(ctx, k{}, "tenant-a")
		_, rows, err := s.List(ctx, where.T(ctxA))
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.Equal(t, 1, rows[0].Balance)

		// 取不到租户（值为空）时必须报错，不能生成 name = '' 的过滤条件
		ctxNone := context.WithValue(ctx, k{}, "")
		_, rows, err = s.List(ctx, where.T(ctxNone))
		require.ErrorIs(t, err, where.ErrEmptyTenant)
		require.Empty(t, rows)

		require.NotPanics(t, func() { _ = where.F("name") })
		_, rows, err = s.List(ctx, where.F("name"))
		require.ErrorIs(t, err, where.ErrInvalidFilter)
		require.Empty(t, rows)
		require.ErrorIs(t, s.Delete(ctx, where.F("name")), where.ErrInvalidFilter)
		n, err := s.Count(ctx, nil)
		require.NoError(t, err)
		require.EqualValues(t, 2, n, "rejected Delete must not touch any tenant's data")
	})
}
