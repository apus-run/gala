// Package sqlx wraps jmoiron/sqlx with a Provider, an option-configured
// constructor, and reflection-built Insert / Update for Modeler types | 在 jmoiron/sqlx 之上提供 Provider、选项式构造，以及基于反射为 Modeler 生成的 Insert / Update.
//
// # Notes | 说明
//
// NewDB parses the DSN with dburl, connects, pings, and applies pool settings
// (defaults: 25 open / 5 idle conns, 30 min lifetime, 5 min idle time).
// Connections are opened in sqlx Unsafe mode. Insert builds one full-row
// INSERT and drops a zero primary key so the database can auto-increment;
// Update sets every non-key column mapped by reflectx and keys on the primary
// key — partial updates and other WHERE clauses are not supported, and
// nested (non-embedded) struct fields are rejected.
// NewDB 用 dburl 解析 DSN，连接、PING 并应用连接池配置（默认 25 打开 / 5 空闲，
// 30 分钟存活，5 分钟空闲）。连接以 sqlx Unsafe 模式打开。Insert 生成整行 INSERT，
// 主键为零值时剔除交由数据库自增；Update 设置 reflectx 映射出的所有非主键列并以
// 主键为条件 —— 不支持部分更新和其它 WHERE 条件，嵌套（非内嵌）结构体字段会被
// 拒绝。
//
// # Features | 功能
//
// - NewDB from DSN with pool options | 从 DSN 构造并配置连接池
// - Transaction with rollback on error and panic | 出错 / panic 自动回滚的事务封装
// - Insert / Update on both DB and Tx | DB 与 Tx 上都可用的 Insert / Update
// - Embedded sqlx handle stays fully usable | 内嵌的 sqlx 句柄仍可完整使用
//
// # Example | 示例
//
//	p, err := sqlx.NewDB(sqlx.WithDSN("postgres://user:pass@host/app"))
//	defer p.Close()
//	if err != nil {
//		return err
//	}
//	_, err = p.DB(ctx).InsertContext(ctx, &User{Name: "alice"})
package sqlx

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/jmoiron/sqlx/reflectx"
	"github.com/xo/dburl"
)

var _ Provider = (*provider)(nil)

type provider struct {
	db *DB
}

// NewDB opens a database from options: parse the DSN, connect, ping, and apply
// pool settings | NewDB 从选项打开数据库：解析 DSN、连接、PING 并应用连接池配置.
func NewDB(opts ...Option) (Provider, error) {
	o := NewOptions(opts...)

	u, err := dburl.Parse(o.dsn)
	if err != nil {
		return nil, fmt.Errorf("unable to parse database URL: %w", err)
	}

	sdb, err := Connect(u.Driver, u.DSN)
	if err != nil {
		return nil, fmt.Errorf("unable to connect to database: %w", err)
	}

	// Apply supplies defaults; pass explicit non-positive values through to database/sql.
	// Apply 提供默认值；显式传入的非正值会直接透传给 database/sql.
	sdb.SetMaxOpenConns(o.maxOpenConns)
	sdb.SetMaxIdleConns(o.maxIdleConns)
	sdb.SetConnMaxLifetime(o.connMaxLifetime)
	sdb.SetConnMaxIdleTime(o.connMaxIdleTime)

	return &provider{
		db: sdb,
	}, nil
}

// DB implements Provider | DB 实现 Provider 接口.
func (p *provider) DB(_ context.Context) *DB {
	return p.db
}

// TX implements Provider by delegating to Transaction | TX 委托 Transaction 实现 Provider 接口.
func (p *provider) TX(ctx context.Context, fn func(ctx context.Context, tx *Tx) error) error {
	return p.db.Transaction(ctx, fn)
}

// Close implements Provider, tolerating a nil handle | Close 实现 Provider 接口，容忍 nil 句柄.
func (p *provider) Close() error {
	if p.db != nil && p.db.DB != nil {
		return p.db.Close()
	}
	return nil
}

// MustConnect connects to a database and panics on error | MustConnect 连接数据库，出错时 panic.
func MustConnect(driverName, dataSourceName string) *DB {
	sqlxdb := sqlx.MustConnect(driverName, dataSourceName)
	sqlxdb = sqlxdb.Unsafe()
	return &DB{sqlxdb}
}

// Connect to a database and verify with a ping | Connect 连接数据库并用 PING 验证.
func Connect(driverName, dataSourceName string) (*DB, error) {
	sqlxdb, err := sqlx.Connect(driverName, dataSourceName)
	if err != nil {
		return nil, err
	}
	sqlxdb = sqlxdb.Unsafe()
	return &DB{sqlxdb}, err
}

// MustBegin return our extended *Tx | MustBegin 返回扩展的 *Tx.
func (db *DB) MustBegin() *Tx {
	tx := db.DB.MustBegin()
	return &Tx{tx}
}

// Beginx return our extended *Tx | Beginx 返回扩展的 *Tx.
func (db *DB) Beginx() (*Tx, error) {
	tx, err := db.DB.Beginx()
	if err != nil {
		return nil, err
	}
	return &Tx{tx}, nil
}

// MustBeginTxx return our extended *Tx and panics on error | MustBeginTxx 返回扩展的 *Tx，出错时 panic.
func (db *DB) MustBeginTxx(ctx context.Context, opts *sql.TxOptions) *Tx {
	tx, err := db.DB.BeginTxx(ctx, opts)
	if err != nil {
		panic(err)
	}
	return &Tx{tx}
}

// BeginTxx return our extended *Tx | BeginTxx 返回扩展的 *Tx.
func (db *DB) BeginTxx(ctx context.Context, opts *sql.TxOptions) (*Tx, error) {
	tx, err := db.DB.BeginTxx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &Tx{tx}, nil
}

// InsertContext generates and executes insert query | InsertContext 生成并执行插入查询.
func (db *DB) InsertContext(ctx context.Context, m Modeler) (sql.Result, error) {
	return insert(ctx, db, m)
}

// Insert  generates and executes insert query without context | Insert 不带 ctx 生成并执行插入查询.
func (db *DB) Insert(m Modeler) (sql.Result, error) {
	return db.InsertContext(context.Background(), m)
}

// UpdateContext generates and executes update query | UpdateContext 生成并执行更新查询.
func (db *DB) UpdateContext(ctx context.Context, m Modeler) (sql.Result, error) {
	return update(ctx, db, m)
}

// Update  generates and executes update query without context | Update 不带 ctx 生成并执行更新查询.
func (db *DB) Update(m Modeler) (sql.Result, error) {
	return db.UpdateContext(context.Background(), m)
}

// Transaction executes a function in a transaction, rolling back on error or
// panic | Transaction 在事务中执行 fn，出错或 panic 时回滚.
func (db *DB) Transaction(ctx context.Context, fn func(ctx context.Context, tx *Tx) error) error {
	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}

	defer func() {
		if v := recover(); v != nil {
			_ = tx.Rollback()
			panic(v)
		}
	}()

	if err = fn(ctx, tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("tx error: %w; rollback error: %v", err, rbErr)
		}
		return err
	}

	return tx.Commit()
}

// InsertContext generates and executes insert query | InsertContext 生成并执行插入查询.
func (tx *Tx) InsertContext(ctx context.Context, m Modeler) (sql.Result, error) {
	return insert(ctx, tx, m)
}

// Insert generates and executes insert query without context | Insert 不带 ctx 生成并执行插入查询.
func (tx *Tx) Insert(m Modeler) (sql.Result, error) {
	return tx.InsertContext(context.Background(), m)
}

// UpdateContext generates and executes update query | UpdateContext 生成并执行更新查询.
func (tx *Tx) UpdateContext(ctx context.Context, m Modeler) (sql.Result, error) {
	return update(ctx, tx, m)
}

// Update  generates and executes update query without context | Update 不带 ctx 生成并执行更新查询.
func (tx *Tx) Update(m Modeler) (sql.Result, error) {
	return tx.UpdateContext(context.Background(), m)
}

// Commit is commit transaction | Commit 提交事务.
func (tx *Tx) Commit() error {
	return tx.Tx.Commit()
}

// Rollback is rollback transaction | Rollback 回滚事务.
func (tx *Tx) Rollback() error {
	return tx.Tx.Rollback()
}

// GetMapper return the Mapper object | GetMapper 返回 Mapper 对象.
func (db *DB) GetMapper() *reflectx.Mapper {
	return db.Mapper
}

// GetMapper return the Mapper object | GetMapper 返回 Mapper 对象.
func (tx *Tx) GetMapper() *reflectx.Mapper {
	return tx.Mapper
}

func insert(ctx context.Context, db mapExecer, m Modeler) (sql.Result, error) {
	names, args, err := bindModeler(m, db.GetMapper())
	if err != nil {
		return nil, err
	}

	keyName := m.KeyName()
	for i := range len(names) {
		if names[i] != keyName {
			continue
		}
		v := reflect.ValueOf(args[i])
		// Drop a zero primary key so the database can auto-increment; keep a non-zero one.
		// 主键为零值时剔除，交由数据库自增；非零值时保留
		if !v.IsValid() || v.IsZero() {
			args = append(args[:i], args[i+1:]...)
			names = append(names[:i], names[i+1:]...)
		}
		break
	}

	if len(names) == 0 {
		return nil, fmt.Errorf("model %T has only zero key %q: no column to insert", m, keyName)
	}

	query := "INSERT INTO " + m.TableName() + "(" + strings.Join(names, ",") + ") VALUES (" + strings.Repeat(",?", len(names))[1:] + ")"
	query = db.Rebind(query)
	return db.ExecContext(ctx, query, args...)
}

func update(ctx context.Context, db mapExecer, m Modeler) (sql.Result, error) {
	names, args, err := bindModeler(m, db.GetMapper())
	if err != nil {
		return nil, err
	}

	var setClauses []string
	var setArgs []any
	var idArg any
	keyName := m.KeyName()

	for i, name := range names {
		if name == keyName {
			idArg = args[i]
			continue
		}
		setClauses = append(setClauses, name+"=?")
		setArgs = append(setArgs, args[i])
	}

	if len(setClauses) == 0 {
		return nil, fmt.Errorf("model %T has no non-key column to update", m)
	}

	query := "UPDATE " + m.TableName() + " SET " + strings.Join(setClauses, ",") + " WHERE " + keyName + " = ?"
	setArgs = append(setArgs, idArg)
	query = db.Rebind(query)
	return db.ExecContext(ctx, query, setArgs...)
}

func bindModeler(mod Modeler, m *reflectx.Mapper) ([]string, []any, error) {
	if mod == nil {
		return nil, nil, fmt.Errorf("nil model")
	}

	// Validate before calling model methods or asking the mapper to inspect the type.
	// 在调用模型方法或让 mapper 检查类型之前先做校验.
	v := reflect.ValueOf(mod)
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return nil, nil, fmt.Errorf("nil modeler %T", mod)
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil, nil, fmt.Errorf("model %T must be a struct or pointer to struct", mod)
	}

	names := []string{}
	for k := range m.TypeMap(v.Type()).Names {
		names = append(names, k)
	}
	slices.Sort(names)

	for _, name := range names {
		if strings.Contains(name, ".") {
			return nil, nil, fmt.Errorf("nested struct field %q of %T not supported, use embedded (flattened) fields", name, mod)
		}
	}
	args, err := bindArgs(names, v, m)
	if err != nil {
		return nil, nil, err
	}
	if !slices.Contains(names, mod.KeyName()) {
		return nil, nil, fmt.Errorf("key column %q not found in %T", mod.KeyName(), mod)
	}

	return names, args, nil
}

func bindArgs(names []string, v reflect.Value, m *reflectx.Mapper) ([]any, error) {
	arglist := make([]any, 0, len(names))

	err := m.TraversalsByNameFunc(v.Type(), names, func(i int, t []int) error {
		if len(t) == 0 {
			return fmt.Errorf("could not find field %q in type %s", names[i], v.Type())
		}

		val := v
		for _, index := range t {
			val = reflect.Indirect(val)
			if !val.IsValid() {
				return fmt.Errorf("nil embedded pointer for field %q in %s", names[i], v.Type())
			}
			val = val.Field(index)
		}
		arglist = append(arglist, val.Interface())

		return nil
	})

	return arglist, err
}
