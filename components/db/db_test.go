package db

import (
	"context"
	"errors"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/plugin/dbresolver"
)

// DB provider tests
func TestNewDB(t *testing.T) {
	prov, err := NewDB(sqlite.Open(":memory:"))
	if err != nil {
		t.Fatalf("NewDB failed: %v", err)
	}
	if prov == nil {
		t.Fatal("NewDB returned nil provider")
	}
}

func TestProvider_NewSession(t *testing.T) {
	prov, _ := NewDB(sqlite.Open(":memory:"))
	ctx := context.Background()
	db := prov.(*provider).DB(ctx)
	if db == nil {
		t.Fatal("NewSession returned nil")
	}
	if db.Statement.Context != ctx {
		t.Fatal("NewSession did not set context")
	}
}

func TestProvider_Transaction(t *testing.T) {
	prov, _ := NewDB(sqlite.Open(":memory:"))
	ctx := context.Background()
	called := false
	err := prov.(*provider).TX(ctx, func(ctx context.Context) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("Transaction failed: %v", err)
	}
	if !called {
		t.Fatal("Transaction did not call function")
	}
}

func TestProvider_Transaction_Error(t *testing.T) {
	prov, _ := NewDB(sqlite.Open(":memory:"))
	ctx := context.Background()
	testErr := errors.New("test error")
	err := prov.(*provider).TX(ctx, func(ctx context.Context) error {
		return testErr
	})
	if err == nil || err.Error() != testErr.Error() {
		t.Fatalf("Transaction did not propagate error, got: %v", err)
	}
}

type txItem struct {
	ID   uint
	Name string
}

func openTxDB(t *testing.T, name ...string) Provider {
	t.Helper()
	dsn := "file:" + t.Name() + strings.Join(name, "") + "?mode=memory&cache=shared"
	prov, err := NewDB(sqlite.Open(dsn))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = prov.Close() })
	if err := prov.DB(context.Background()).AutoMigrate(&txItem{}); err != nil {
		t.Fatal(err)
	}
	return prov
}

// DB(ctx) 在 TX 传入的 ctx 下落在事务上：fn 返回错误时，经 DB(ctx) 的写入一并回滚。
func TestProvider_TXPropagatesThroughContext(t *testing.T) {
	prov := openTxDB(t)
	ctx := context.Background()
	rollback := errors.New("rollback")

	err := prov.TX(ctx, func(ctx context.Context) error {
		if err := prov.DB(ctx).Create(&txItem{Name: "a"}).Error; err != nil {
			return err
		}
		var n int64
		if err := prov.DB(ctx).Model(&txItem{}).Count(&n).Error; err != nil || n != 1 {
			t.Fatalf("in-tx count = %d, err = %v, want 1", n, err)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("TX err = %v, want rollback", err)
	}

	var n int64
	prov.DB(ctx).Model(&txItem{}).Count(&n)
	if n != 0 {
		t.Fatalf("after rollback count = %d, want 0", n)
	}
}

// 嵌套 TX 走 savepoint：内层失败只回滚内层，外层提交。
func TestProvider_TXNested(t *testing.T) {
	prov := openTxDB(t)
	ctx := context.Background()

	err := prov.TX(ctx, func(ctx context.Context) error {
		if err := prov.DB(ctx).Create(&txItem{Name: "outer"}).Error; err != nil {
			return err
		}
		_ = prov.TX(ctx, func(ctx context.Context) error {
			prov.DB(ctx).Create(&txItem{Name: "inner"})
			return errors.New("inner fails")
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	var names []string
	prov.DB(ctx).Model(&txItem{}).Pluck("name", &names)
	if len(names) != 1 || names[0] != "outer" {
		t.Fatalf("names = %v, want [outer]", names)
	}
}

// 调用方传入的 Config 不能被覆盖，只补开 TranslateError。
func TestWithTranslateError(t *testing.T) {
	cfg := &gorm.Config{SkipDefaultTransaction: true}
	opts := withTranslateError([]gorm.Option{cfg})
	if len(opts) != 1 || !cfg.TranslateError || !cfg.SkipDefaultTransaction {
		t.Fatalf("opts = %d, cfg = %+v", len(opts), cfg)
	}

	opts = withTranslateError(nil)
	if c, ok := opts[0].(*gorm.Config); len(opts) != 1 || !ok || !c.TranslateError {
		t.Fatalf("opts = %v", opts)
	}
}

// NewDB 总是开启 TranslateError：唯一键冲突统一为 gorm.ErrDuplicatedKey。
func TestNewDB_TranslatesErrors(t *testing.T) {
	prov := openTxDB(t)
	ctx := context.Background()
	if err := prov.DB(ctx).Create(&txItem{ID: 1}).Error; err != nil {
		t.Fatal(err)
	}
	err := prov.DB(ctx).Create(&txItem{ID: 1}).Error
	if !errors.Is(err, gorm.ErrDuplicatedKey) {
		t.Fatalf("err = %v, want gorm.ErrDuplicatedKey", err)
	}
}

// 两个 Provider 的事务互不串用：mainDB.TX 的 ctx 交给 statsDB.DB，
// 写入落在 statsDB 自己的库，不随 mainDB 回滚。
func TestProvider_TXIsolatedPerProvider(t *testing.T) {
	mainDB := openTxDB(t, "main")
	statsDB := openTxDB(t, "stats")
	ctx := context.Background()
	rollback := errors.New("rollback")

	err := mainDB.TX(ctx, func(ctx context.Context) error {
		if err := mainDB.DB(ctx).Create(&txItem{Name: "main"}).Error; err != nil {
			return err
		}
		if err := statsDB.DB(ctx).Create(&txItem{Name: "stats"}).Error; err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("TX err = %v, want rollback", err)
	}

	var mainNames, statsNames []string
	mainDB.DB(ctx).Model(&txItem{}).Pluck("name", &mainNames)
	statsDB.DB(ctx).Model(&txItem{}).Pluck("name", &statsNames)
	if len(mainNames) != 0 {
		t.Fatalf("main = %v, want [] (rolled back)", mainNames)
	}
	if len(statsNames) != 1 || statsNames[0] != "stats" {
		t.Fatalf("stats = %v, want [stats]", statsNames)
	}
}

type softItem struct {
	ID        uint
	Name      string
	DeletedAt gorm.DeletedAt
}

func TestNewDB_OpenError(t *testing.T) {
	if _, err := NewDB(sqlite.Open("/nonexistent-dir/x.db")); err == nil {
		t.Fatal("NewDB on an unopenable path should fail")
	}
}

type fakeProvider struct{ Provider }

func TestUnwrap(t *testing.T) {
	prov := openTxDB(t)
	if g, ok := Unwrap(prov); !ok || g == nil {
		t.Fatalf("Unwrap(real) = %v, %v", g, ok)
	}
	if g, ok := Unwrap(fakeProvider{}); ok || g != nil {
		t.Fatalf("Unwrap(foreign) = %v, %v, want nil,false", g, ok)
	}
	if g, ok := Unwrap(&provider{}); ok || g != nil {
		t.Fatalf("Unwrap(empty provider) = %v, %v, want nil,false", g, ok)
	}
}

func TestProvider_Close(t *testing.T) {
	if err := (&provider{}).Close(); err != nil {
		t.Fatalf("Close on empty provider = %v, want nil", err)
	}

	prov, err := NewDB(sqlite.Open("file:" + t.Name() + "?mode=memory&cache=shared"))
	if err != nil {
		t.Fatal(err)
	}
	if err := prov.Close(); err != nil {
		t.Fatalf("Close = %v", err)
	}
	if err := prov.DB(context.Background()).Exec("SELECT 1").Error; err == nil {
		t.Fatal("query after Close should fail")
	}
}

func TestApply(t *testing.T) {
	o := Apply(WithMaster(), Debug(), WithDeleted(), WithSelectForUpdate())
	if !o.withMaster || !o.debug || !o.withDeleted || !o.selectForUpdate {
		t.Fatalf("Apply did not set all flags: %+v", o)
	}
	if o := Apply(); o.withMaster || o.debug || o.withDeleted || o.selectForUpdate || o.tx != nil {
		t.Fatalf("Apply() should be the zero Options, got %+v", o)
	}
}

// WithTransaction 让 DB 落在调用方已有的事务上。
func TestDB_WithTransaction(t *testing.T) {
	prov := openTxDB(t)
	ctx := context.Background()
	raw, _ := Unwrap(prov)

	tx := raw.Begin()
	if err := prov.DB(ctx, WithTransaction(tx)).Create(&txItem{Name: "x"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback().Error; err != nil {
		t.Fatal(err)
	}

	var n int64
	prov.DB(ctx).Model(&txItem{}).Count(&n)
	if n != 0 {
		t.Fatalf("count = %d, want 0 (row written through the rolled-back tx)", n)
	}
}

func TestDB_WithDeleted(t *testing.T) {
	prov := openTxDB(t)
	ctx := context.Background()
	if err := prov.DB(ctx).AutoMigrate(&softItem{}); err != nil {
		t.Fatal(err)
	}
	it := &softItem{Name: "gone"}
	prov.DB(ctx).Create(it)
	prov.DB(ctx).Delete(it)

	var visible, all int64
	prov.DB(ctx).Model(&softItem{}).Count(&visible)
	prov.DB(ctx, WithDeleted()).Model(&softItem{}).Count(&all)
	if visible != 0 || all != 1 {
		t.Fatalf("visible = %d, all = %d, want 0 and 1", visible, all)
	}
}

func TestDB_WithSelectForUpdate(t *testing.T) {
	prov := openTxDB(t)
	ctx := context.Background()

	stmt := prov.DB(ctx, WithSelectForUpdate()).Statement
	c, ok := stmt.Clauses["FOR"]
	if !ok {
		t.Fatal("WithSelectForUpdate did not add a FOR clause")
	}
	if l, _ := c.Expression.(clause.Locking); l.Strength != "UPDATE" {
		t.Fatalf("locking = %+v, want UPDATE", c.Expression)
	}
	if _, ok := prov.DB(ctx).Statement.Clauses["FOR"]; ok {
		t.Fatal("FOR clause leaked into a plain DB(ctx)")
	}
}

func TestDB_Debug(t *testing.T) {
	prov := openTxDB(t)
	ctx := context.Background()

	var n int64
	if err := prov.DB(ctx, Debug()).Model(&txItem{}).Count(&n).Error; err != nil {
		t.Fatalf("Debug() session query failed: %v", err)
	}
	plain, debug := prov.DB(ctx).Logger, prov.DB(ctx, Debug()).Logger
	if plain == debug {
		t.Fatal("Debug() should switch to a different logger")
	}
}

// WithMaster 在读写分离下强制读主库：默认读副本，WithMaster 读主库。
func TestDB_WithMaster(t *testing.T) {
	ctx := context.Background()
	primary := "file:" + t.Name() + "primary?mode=memory&cache=shared"
	replica := "file:" + t.Name() + "replica?mode=memory&cache=shared"

	prov, err := NewDB(sqlite.Open(primary))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = prov.Close() })
	rep, err := gorm.Open(sqlite.Open(replica), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	repSQL, _ := rep.DB()
	t.Cleanup(func() { _ = repSQL.Close() })

	raw, _ := Unwrap(prov)
	for _, g := range []*gorm.DB{raw, rep} {
		if err := g.AutoMigrate(&txItem{}); err != nil {
			t.Fatal(err)
		}
	}
	if err := raw.Use(dbresolver.Register(dbresolver.Config{
		Replicas: []gorm.Dialector{sqlite.Open(replica)},
	})); err != nil {
		t.Fatal(err)
	}

	rep.Create(&txItem{Name: "on-replica"})
	prov.DB(ctx, WithMaster()).Create(&txItem{Name: "on-primary"})

	var def, master []string
	prov.DB(ctx).Model(&txItem{}).Pluck("name", &def)
	prov.DB(ctx, WithMaster()).Model(&txItem{}).Pluck("name", &master)
	if len(def) != 1 || def[0] != "on-replica" {
		t.Fatalf("default read = %v, want [on-replica]", def)
	}
	if len(master) != 1 || master[0] != "on-primary" {
		t.Fatalf("WithMaster read = %v, want [on-primary]", master)
	}
}
