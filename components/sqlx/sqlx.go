package sqlx

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/jmoiron/sqlx/reflectx"
	"github.com/xo/dburl"
)

var _ Provider = (*provider)(nil)

type provider struct {
	db *DB
}

func NewDB(opts ...Option) (Provider, error) {
	o := Apply(opts...)

	u, err := dburl.Parse(o.dsn)
	if err != nil {
		return nil, fmt.Errorf("unable to parse database URL: %v", err)
	}

	sdb, err := Connect(u.Driver, u.DSN)
	if err != nil {
		return nil, fmt.Errorf("unable to connect to database: %v", err)
	}

	if o.maxOpenConns > 0 {
		sdb.SetMaxOpenConns(o.maxOpenConns)
	}
	if o.maxIdleConns > 0 {
		sdb.SetMaxIdleConns(o.maxIdleConns)
	}
	if o.connMaxLifetime > 0 {
		sdb.SetConnMaxLifetime(o.connMaxLifetime)
	}
	if o.connMaxIdleTime > 0 {
		sdb.SetConnMaxIdleTime(o.connMaxIdleTime)
	}

	return &provider{
		db: sdb,
	}, nil
}

func (p *provider) DB(_ context.Context) *DB {
	return p.db
}

func (p *provider) TX(ctx context.Context, fn func(ctx context.Context, tx *Tx) error) error {
	return p.db.Transaction(ctx, fn)
}

func (p *provider) Close() error {
	if p.db != nil && p.db.DB != nil {
		return p.db.Close()
	}
	return nil
}

// MustConnect connects to a database and panics on error.
func MustConnect(driverName, dataSourceName string) *DB {
	sqlxdb := sqlx.MustConnect(driverName, dataSourceName)
	sqlxdb = sqlxdb.Unsafe()
	return &DB{sqlxdb}
}

// Connect to a database and verify with a ping.
func Connect(driverName, dataSourceName string) (*DB, error) {
	sqlxdb, err := sqlx.Connect(driverName, dataSourceName)
	if err != nil {
		return nil, err
	}
	sqlxdb = sqlxdb.Unsafe()
	return &DB{sqlxdb}, err
}

// MustBegin return our extended *Tx
func (db *DB) MustBegin() *Tx {
	tx := db.DB.MustBegin()
	return &Tx{tx}
}

// Beginx return our extended *Tx
func (db *DB) Beginx() (*Tx, error) {
	tx, err := db.DB.Beginx()
	if err != nil {
		return nil, err
	}
	return &Tx{tx}, nil
}

// MustBeginTxx return our extended *Tx and panics on error
func (db *DB) MustBeginTxx(ctx context.Context, opts *sql.TxOptions) *Tx {
	tx, err := db.DB.BeginTxx(ctx, opts)
	if err != nil {
		panic(err)
	}
	return &Tx{tx}
}

// BeginTxx return our extended *Tx
func (db *DB) BeginTxx(ctx context.Context, opts *sql.TxOptions) (*Tx, error) {
	tx, err := db.DB.BeginTxx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &Tx{tx}, nil
}

// InsertContext generates and executes insert query.
func (db *DB) InsertContext(ctx context.Context, m Modeler) (sql.Result, error) {
	return insert(ctx, db, m)
}

// Insert  generates and executes insert query without context.
func (db *DB) Insert(m Modeler) (sql.Result, error) {
	return db.InsertContext(context.Background(), m)
}

// UpdateContext generates and executes update query.
func (db *DB) UpdateContext(ctx context.Context, m Modeler) (sql.Result, error) {
	return update(ctx, db, m)
}

// Update  generates and executes update query without context.
func (db *DB) Update(m Modeler) (sql.Result, error) {
	return db.UpdateContext(context.Background(), m)
}

// Transaction executes a function in a transaction.
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

// InsertContext generates and executes insert query.
func (tx *Tx) InsertContext(ctx context.Context, m Modeler) (sql.Result, error) {
	return insert(ctx, tx, m)
}

// Insert generates and executes insert query without context.
func (tx *Tx) Insert(m Modeler) (sql.Result, error) {
	return tx.InsertContext(context.Background(), m)
}

// UpdateContext generates and executes update query.
func (tx *Tx) UpdateContext(ctx context.Context, m Modeler) (sql.Result, error) {
	return update(ctx, tx, m)
}

// Update  generates and executes update query without context.
func (tx *Tx) Update(m Modeler) (sql.Result, error) {
	return tx.UpdateContext(context.Background(), m)
}

// Commit is commit transaction
func (tx *Tx) Commit() error {
	return tx.Tx.Commit()
}

// Rollback is rollback transaction
func (tx *Tx) Rollback() error {
	return tx.Tx.Rollback()
}

// GetMapper return the Mapper object.
func (db *DB) GetMapper() *reflectx.Mapper {
	return db.Mapper
}

// GetMapper return the Mapper object.
func (tx *Tx) GetMapper() *reflectx.Mapper {
	return tx.Mapper
}

func insert(ctx context.Context, db mapExecer, m Modeler) (sql.Result, error) {
	names, args, err := bindModeler(m, db.GetMapper())
	if err != nil {
		return nil, err
	}

	for i := 0; i < len(names); i++ {
		if names[i] != m.KeyName() {
			continue
		}
		v := reflect.ValueOf(args[i])
		// 主键为零值时剔除，交由数据库自增；非零值时保留
		if !v.IsValid() || v.IsZero() {
			args = append(args[:i], args[i+1:]...)
			names = append(names[:i], names[i+1:]...)
		}
		break
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

	for i, name := range names {
		if name == m.KeyName() {
			idArg = args[i]
			continue
		}
		setClauses = append(setClauses, name+"=?")
		setArgs = append(setArgs, args[i])
	}

	query := "UPDATE " + m.TableName() + " SET " + strings.Join(setClauses, ",") + " WHERE " + m.KeyName() + " = ?"
	setArgs = append(setArgs, idArg)
	query = db.Rebind(query)
	return db.ExecContext(ctx, query, setArgs...)
}

func bindModeler(arg any, m *reflectx.Mapper) ([]string, []any, error) {
	t := reflect.TypeOf(arg)
	names := []string{}
	for k := range m.TypeMap(t).Names {
		names = append(names, k)
	}
	sort.Stable(sort.StringSlice(names))
	args, err := bindArgs(names, arg, m)
	if err != nil {
		return nil, nil, err
	}

	return names, args, nil
}

func bindArgs(names []string, arg any, m *reflectx.Mapper) ([]any, error) {
	arglist := make([]any, 0, len(names))

	// 解引用指针，获取实际值
	v := reflect.ValueOf(arg)
	for v = reflect.ValueOf(arg); v.Kind() == reflect.Ptr; {
		v = v.Elem()
	}

	err := m.TraversalsByNameFunc(v.Type(), names, func(i int, t []int) error {
		if len(t) == 0 {
			// fix: 使用 %T 替代 %#v，避免大型结构体导致错误信息过于冗长
			return fmt.Errorf("could not find field %q in type %T", names[i], arg)
		}

		val := reflectx.FieldByIndexesReadOnly(v, t)
		arglist = append(arglist, val.Interface())

		return nil
	})

	return arglist, err
}
