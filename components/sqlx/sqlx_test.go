package sqlx

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// -----------------------------------------------------------------------------
// 测试模型
// -----------------------------------------------------------------------------

// User 是测试用 Modeler，使用 db 标签映射列名
type User struct {
	ID    int64  `db:"id"`
	Name  string `db:"name"`
	Email string `db:"email"`
	Age   int    `db:"age"`
}

func (u *User) TableName() string { return "users" }
func (u *User) KeyName() string   { return "id" }

// Product 是另一个测试用 Modeler
type Product struct {
	ProductID int64   `db:"product_id"`
	Title     string  `db:"title"`
	Price     float64 `db:"price"`
}

func (p *Product) TableName() string { return "products" }
func (p *Product) KeyName() string   { return "product_id" }

// brokenModel 用于测试字段不能映射的失败场景
type brokenModel struct {
	ID int64 `db:"id"`
}

func (b *brokenModel) TableName() string { return "broken" }

// 故意返回一个不存在的列名，触发 bindArgs 中的字段查找失败
func (b *brokenModel) KeyName() string { return "missing" }

// -----------------------------------------------------------------------------
// 测试辅助函数
// -----------------------------------------------------------------------------

// newTestDB 直接通过 Connect 建立一个 sqlite 内存数据库连接并初始化测试 schema。
func newTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Connect("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mustExec(t, db, `CREATE TABLE users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		email TEXT NOT NULL,
		age INTEGER NOT NULL DEFAULT 0
	)`)
	mustExec(t, db, `CREATE TABLE products (
		product_id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT NOT NULL,
		price REAL NOT NULL DEFAULT 0
	)`)
	return db
}

func mustExec(t *testing.T, db *DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("exec %q failed: %v", query, err)
	}
}

func countRows(t *testing.T, db *DB, table string) int {
	t.Helper()
	var n int
	if err := db.Get(&n, "SELECT COUNT(*) FROM "+table); err != nil {
		t.Fatalf("count rows in %s: %v", table, err)
	}
	return n
}

func getUser(t *testing.T, db *DB, id int64) *User {
	t.Helper()
	u := &User{}
	if err := db.Get(u, "SELECT id, name, email, age FROM users WHERE id = ?", id); err != nil {
		t.Fatalf("get user %d: %v", id, err)
	}
	return u
}

// -----------------------------------------------------------------------------
// Options / Apply / WithXxx
// -----------------------------------------------------------------------------

func TestApply_Defaults(t *testing.T) {
	o := Apply()
	if o.dsn != "" {
		t.Errorf("default dsn want empty, got %q", o.dsn)
	}
	if o.maxOpenConns != 25 {
		t.Errorf("default maxOpenConns want 25, got %d", o.maxOpenConns)
	}
	if o.maxIdleConns != 5 {
		t.Errorf("default maxIdleConns want 5, got %d", o.maxIdleConns)
	}
	if o.connMaxLifetime != 30*time.Minute {
		t.Errorf("default connMaxLifetime want 30m, got %v", o.connMaxLifetime)
	}
	if o.connMaxIdleTime != 5*time.Minute {
		t.Errorf("default connMaxIdleTime want 5m, got %v", o.connMaxIdleTime)
	}
}

func TestApply_WithOptions(t *testing.T) {
	o := Apply(
		WithDSN("sqlite3://:memory:"),
		WithMaxOpenConns(50),
		WithMaxIdleConns(10),
		WithConnMaxLifetime(time.Hour),
		WithConnMaxIdleTime(10*time.Minute),
	)
	if o.dsn != "sqlite3://:memory:" {
		t.Errorf("dsn want sqlite3://:memory:, got %q", o.dsn)
	}
	if o.maxOpenConns != 50 {
		t.Errorf("maxOpenConns want 50, got %d", o.maxOpenConns)
	}
	if o.maxIdleConns != 10 {
		t.Errorf("maxIdleConns want 10, got %d", o.maxIdleConns)
	}
	if o.connMaxLifetime != time.Hour {
		t.Errorf("connMaxLifetime want 1h, got %v", o.connMaxLifetime)
	}
	if o.connMaxIdleTime != 10*time.Minute {
		t.Errorf("connMaxIdleTime want 10m, got %v", o.connMaxIdleTime)
	}
}

// -----------------------------------------------------------------------------
// Connect / MustConnect
// -----------------------------------------------------------------------------

func TestConnect_Success(t *testing.T) {
	db, err := Connect("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer db.Close()
	if db.DB == nil {
		t.Fatal("expected embedded sqlx.DB to be non-nil")
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping: %v", err)
	}
}

func TestConnect_BadDriver(t *testing.T) {
	if _, err := Connect("not-a-driver", ":memory:"); err == nil {
		t.Fatal("expected error for unknown driver")
	}
}

func TestMustConnect_Success(t *testing.T) {
	db := MustConnect("sqlite3", ":memory:")
	defer db.Close()
	if db == nil || db.DB == nil {
		t.Fatal("MustConnect returned nil db")
	}
}

func TestMustConnect_PanicsOnBadDriver(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on unknown driver")
		}
	}()
	_ = MustConnect("not-a-driver", ":memory:")
}

// -----------------------------------------------------------------------------
// NewDB / Provider
// -----------------------------------------------------------------------------

func TestNewDB_Success(t *testing.T) {
	p, err := NewDB(
		WithDSN("sqlite3::memory:"),
		WithMaxOpenConns(7),
		WithMaxIdleConns(3),
		WithConnMaxLifetime(15*time.Minute),
		WithConnMaxIdleTime(2*time.Minute),
	)
	if err != nil {
		t.Fatalf("NewDB failed: %v", err)
	}
	defer p.Close()

	if got := p.DB(context.Background()); got == nil {
		t.Fatal("Provider.DB returned nil")
	}
}

func TestNewDB_BadDSN(t *testing.T) {
	if _, err := NewDB(WithDSN("not a url ::: %%%")); err == nil {
		t.Fatal("expected parse error for bad DSN")
	}
}

func TestNewDB_UnknownDriver(t *testing.T) {
	// dburl 解析成功但驱动未注册时应返回连接错误
	if _, err := NewDB(WithDSN("nodriver://localhost/x")); err == nil {
		t.Fatal("expected error for unknown driver scheme")
	}
}

func TestProvider_Close_NilSafe(t *testing.T) {
	// 即使内部 db 为 nil，Close 也不应 panic
	p := &provider{db: nil}
	if err := p.Close(); err != nil {
		t.Fatalf("Close on nil db: %v", err)
	}
	p2 := &provider{db: &DB{}}
	if err := p2.Close(); err != nil {
		t.Fatalf("Close on db with nil sqlx.DB: %v", err)
	}
}

func TestProvider_TX_CommitsAndPersists(t *testing.T) {
	p, err := NewDB(WithDSN("sqlite3::memory:"))
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer p.Close()

	mustExec(t, p.DB(context.Background()), `CREATE TABLE users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		email TEXT NOT NULL,
		age INTEGER NOT NULL DEFAULT 0
	)`)

	ctx := context.Background()
	err = p.TX(ctx, func(ctx context.Context, tx *Tx) error {
		_, err := tx.InsertContext(ctx, &User{Name: "alice", Email: "a@x.com", Age: 30})
		return err
	})
	if err != nil {
		t.Fatalf("TX failed: %v", err)
	}
	if got := countRows(t, p.DB(ctx), "users"); got != 1 {
		t.Fatalf("rows after commit want 1, got %d", got)
	}
}

// -----------------------------------------------------------------------------
// Insert
// -----------------------------------------------------------------------------

func TestDB_Insert_AutoIncrementPrimaryKey(t *testing.T) {
	db := newTestDB(t)

	res, err := db.Insert(&User{Name: "bob", Email: "bob@x.com", Age: 22})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId: %v", err)
	}
	if id <= 0 {
		t.Fatalf("expected positive auto-increment id, got %d", id)
	}

	got := getUser(t, db, id)
	if got.Name != "bob" || got.Email != "bob@x.com" || got.Age != 22 {
		t.Fatalf("unexpected row: %+v", got)
	}
}

func TestDB_InsertContext_RespectsExplicitPrimaryKey(t *testing.T) {
	db := newTestDB(t)

	res, err := db.InsertContext(context.Background(), &User{ID: 100, Name: "carol", Email: "c@x.com", Age: 40})
	if err != nil {
		t.Fatalf("InsertContext: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId: %v", err)
	}
	if id != 100 {
		t.Fatalf("explicit id want 100, got %d", id)
	}
}

func TestDB_Insert_MultipleRows(t *testing.T) {
	db := newTestDB(t)

	rows := []*User{
		{Name: "u1", Email: "u1@x.com", Age: 10},
		{Name: "u2", Email: "u2@x.com", Age: 20},
		{Name: "u3", Email: "u3@x.com", Age: 30},
	}
	for _, r := range rows {
		if _, err := db.Insert(r); err != nil {
			t.Fatalf("insert %s: %v", r.Name, err)
		}
	}
	if n := countRows(t, db, "users"); n != len(rows) {
		t.Fatalf("rows want %d, got %d", len(rows), n)
	}
}

func TestDB_Insert_DifferentModeler(t *testing.T) {
	db := newTestDB(t)

	res, err := db.Insert(&Product{Title: "widget", Price: 9.99})
	if err != nil {
		t.Fatalf("Insert product: %v", err)
	}
	id, _ := res.LastInsertId()
	if id <= 0 {
		t.Fatalf("expected positive product id, got %d", id)
	}

	var p Product
	if err := db.Get(&p, "SELECT product_id, title, price FROM products WHERE product_id = ?", id); err != nil {
		t.Fatalf("get product: %v", err)
	}
	if p.Title != "widget" || p.Price != 9.99 {
		t.Fatalf("unexpected product row: %+v", p)
	}
}

func TestDB_Insert_BrokenModelReturnsError(t *testing.T) {
	db := newTestDB(t)
	mustExec(t, db, `CREATE TABLE broken (id INTEGER PRIMARY KEY)`)

	if _, err := db.Insert(&brokenModel{ID: 1}); err == nil {
		t.Fatal("expected error when modeler references missing field")
	}
}

// -----------------------------------------------------------------------------
// Update
// -----------------------------------------------------------------------------

func TestDB_Update_ChangesRow(t *testing.T) {
	db := newTestDB(t)

	res, err := db.Insert(&User{Name: "dan", Email: "d@x.com", Age: 1})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	id, _ := res.LastInsertId()

	updated := &User{ID: id, Name: "daniel", Email: "daniel@x.com", Age: 2}
	r, err := db.Update(updated)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	affected, err := r.RowsAffected()
	if err != nil {
		t.Fatalf("RowsAffected: %v", err)
	}
	if affected != 1 {
		t.Fatalf("RowsAffected want 1, got %d", affected)
	}

	got := getUser(t, db, id)
	if got.Name != "daniel" || got.Email != "daniel@x.com" || got.Age != 2 {
		t.Fatalf("unexpected row after update: %+v", got)
	}
}

func TestDB_UpdateContext_NoMatchAffectsZero(t *testing.T) {
	db := newTestDB(t)

	r, err := db.UpdateContext(context.Background(), &User{ID: 9999, Name: "ghost", Email: "g@x.com", Age: 0})
	if err != nil {
		t.Fatalf("UpdateContext: %v", err)
	}
	affected, _ := r.RowsAffected()
	if affected != 0 {
		t.Fatalf("RowsAffected on missing row want 0, got %d", affected)
	}
}

func TestDB_Update_BrokenModelReturnsError(t *testing.T) {
	db := newTestDB(t)
	mustExec(t, db, `CREATE TABLE broken (id INTEGER PRIMARY KEY)`)

	if _, err := db.Update(&brokenModel{ID: 1}); err == nil {
		t.Fatal("expected error when modeler references missing field")
	}
}

// -----------------------------------------------------------------------------
// Tx 上的 Insert / Update
// -----------------------------------------------------------------------------

func TestTx_InsertAndUpdate(t *testing.T) {
	db := newTestDB(t)

	tx, err := db.Beginx()
	if err != nil {
		t.Fatalf("Beginx: %v", err)
	}
	res, err := tx.Insert(&User{Name: "eve", Email: "e@x.com", Age: 50})
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("tx insert: %v", err)
	}
	id, _ := res.LastInsertId()
	if _, err := tx.Update(&User{ID: id, Name: "evelyn", Email: "e@x.com", Age: 51}); err != nil {
		_ = tx.Rollback()
		t.Fatalf("tx update: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	got := getUser(t, db, id)
	if got.Name != "evelyn" || got.Age != 51 {
		t.Fatalf("unexpected row: %+v", got)
	}
}

func TestTx_InsertContext_UpdateContext(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTxx: %v", err)
	}
	res, err := tx.InsertContext(ctx, &User{Name: "frank", Email: "f@x.com", Age: 12})
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("InsertContext: %v", err)
	}
	id, _ := res.LastInsertId()
	if _, err := tx.UpdateContext(ctx, &User{ID: id, Name: "frankie", Email: "f@x.com", Age: 13}); err != nil {
		_ = tx.Rollback()
		t.Fatalf("UpdateContext: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	got := getUser(t, db, id)
	if got.Name != "frankie" || got.Age != 13 {
		t.Fatalf("unexpected row: %+v", got)
	}
}

func TestTx_RollbackDiscardsChanges(t *testing.T) {
	db := newTestDB(t)

	tx, err := db.Beginx()
	if err != nil {
		t.Fatalf("Beginx: %v", err)
	}
	if _, err := tx.Insert(&User{Name: "ghost", Email: "g@x.com", Age: 0}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if n := countRows(t, db, "users"); n != 0 {
		t.Fatalf("rollback should have discarded inserts, got %d rows", n)
	}
}

// -----------------------------------------------------------------------------
// Begin/Beginx/MustBegin/MustBeginTxx/BeginTxx
// -----------------------------------------------------------------------------

func TestDB_Beginx(t *testing.T) {
	db := newTestDB(t)
	tx, err := db.Beginx()
	if err != nil {
		t.Fatalf("Beginx: %v", err)
	}
	if tx == nil || tx.Tx == nil {
		t.Fatal("expected non-nil Tx")
	}
	_ = tx.Rollback()
}

func TestDB_MustBegin(t *testing.T) {
	db := newTestDB(t)
	tx := db.MustBegin()
	if tx == nil {
		t.Fatal("MustBegin returned nil")
	}
	_ = tx.Rollback()
}

func TestDB_BeginTxx(t *testing.T) {
	db := newTestDB(t)
	tx, err := db.BeginTxx(context.Background(), &sql.TxOptions{ReadOnly: false})
	if err != nil {
		t.Fatalf("BeginTxx: %v", err)
	}
	if tx == nil || tx.Tx == nil {
		t.Fatal("expected non-nil Tx")
	}
	_ = tx.Rollback()
}

func TestDB_MustBeginTxx(t *testing.T) {
	db := newTestDB(t)
	tx := db.MustBeginTxx(context.Background(), nil)
	if tx == nil {
		t.Fatal("MustBeginTxx returned nil")
	}
	_ = tx.Rollback()
}

func TestDB_BeginTxx_CanceledContextErrors(t *testing.T) {
	db := newTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := db.BeginTxx(ctx, nil); err == nil {
		t.Fatal("expected error from BeginTxx with canceled context")
	}
}

func TestDB_MustBeginTxx_PanicsOnError(t *testing.T) {
	db := newTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic from MustBeginTxx with canceled context")
		}
	}()
	_ = db.MustBeginTxx(ctx, nil)
}

// -----------------------------------------------------------------------------
// Transaction
// -----------------------------------------------------------------------------

func TestTransaction_CommitOnSuccess(t *testing.T) {
	db := newTestDB(t)

	err := db.Transaction(context.Background(), func(ctx context.Context, tx *Tx) error {
		_, err := tx.InsertContext(ctx, &User{Name: "tx-ok", Email: "ok@x.com", Age: 1})
		return err
	})
	if err != nil {
		t.Fatalf("Transaction: %v", err)
	}
	if n := countRows(t, db, "users"); n != 1 {
		t.Fatalf("rows after commit want 1, got %d", n)
	}
}

func TestTransaction_RollbackOnError(t *testing.T) {
	db := newTestDB(t)

	sentinel := errors.New("boom")
	err := db.Transaction(context.Background(), func(ctx context.Context, tx *Tx) error {
		if _, err := tx.InsertContext(ctx, &User{Name: "tx-fail", Email: "fail@x.com", Age: 1}); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
	if n := countRows(t, db, "users"); n != 0 {
		t.Fatalf("rows after rollback want 0, got %d", n)
	}
}

func TestTransaction_RollbackOnPanic(t *testing.T) {
	db := newTestDB(t)

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic to propagate out of Transaction")
		}
		// 校验事务确实回滚了
		if n := countRows(t, db, "users"); n != 0 {
			t.Fatalf("rows after panic want 0, got %d", n)
		}
	}()

	_ = db.Transaction(context.Background(), func(ctx context.Context, tx *Tx) error {
		_, _ = tx.InsertContext(ctx, &User{Name: "tx-panic", Email: "p@x.com", Age: 1})
		panic("oops")
	})
}

func TestTransaction_BeginErrorPropagates(t *testing.T) {
	db := newTestDB(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := db.Transaction(ctx, func(ctx context.Context, tx *Tx) error {
		t.Fatal("fn should not run when Begin fails")
		return nil
	})
	if err == nil {
		t.Fatal("expected error from Transaction when BeginTxx fails")
	}
}

// -----------------------------------------------------------------------------
// GetMapper
// -----------------------------------------------------------------------------

func TestDB_GetMapper(t *testing.T) {
	db := newTestDB(t)
	m := db.GetMapper()
	if m == nil {
		t.Fatal("DB.GetMapper returned nil")
	}
}

func TestTx_GetMapper(t *testing.T) {
	db := newTestDB(t)
	tx, err := db.Beginx()
	if err != nil {
		t.Fatalf("Beginx: %v", err)
	}
	defer tx.Rollback()
	if m := tx.GetMapper(); m == nil {
		t.Fatal("Tx.GetMapper returned nil")
	}
}

// -----------------------------------------------------------------------------
// bindModeler / bindArgs
// -----------------------------------------------------------------------------

func TestBindModeler_PointerAndValueAndSorted(t *testing.T) {
	db := newTestDB(t)
	mapper := db.GetMapper()

	u := &User{ID: 1, Name: "a", Email: "a@x.com", Age: 9}

	names, args, err := bindModeler(u, mapper)
	if err != nil {
		t.Fatalf("bindModeler(ptr): %v", err)
	}
	wantNames := []string{"age", "email", "id", "name"}
	if !equalStrings(names, wantNames) {
		t.Fatalf("names not sorted: got %v want %v", names, wantNames)
	}
	if len(args) != len(wantNames) {
		t.Fatalf("args len want %d, got %d", len(wantNames), len(args))
	}
	// 验证参数与字段对应（age, email, id, name）
	wantArgs := []any{9, "a@x.com", int64(1), "a"}
	for i := range args {
		if args[i] != wantArgs[i] {
			t.Fatalf("args[%d] want %v, got %v", i, wantArgs[i], args[i])
		}
	}

	// 直接对值类型调用也应工作
	names2, _, err := bindModeler(*u, mapper)
	if err != nil {
		t.Fatalf("bindModeler(value): %v", err)
	}
	if !equalStrings(names2, wantNames) {
		t.Fatalf("names(value) not sorted: got %v", names2)
	}
}

func TestBindArgs_FieldNotFoundError(t *testing.T) {
	db := newTestDB(t)
	mapper := db.GetMapper()

	// 给 User 显式构造一个不存在的列名，应该返回错误
	_, err := bindArgs([]string{"id", "not_a_field"}, &User{ID: 1}, mapper)
	if err == nil {
		t.Fatal("expected error for missing field")
	}
}

// -----------------------------------------------------------------------------
// 帮助函数
// -----------------------------------------------------------------------------

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
