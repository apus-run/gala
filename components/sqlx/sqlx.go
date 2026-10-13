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

func NewDB(opts ...Option) (Provider, error) {
	o := Apply(opts...)

	u, err := dburl.Parse(o.dsn)
	if err != nil {
		return nil, fmt.Errorf("unable to parse database URL: %w", err)
	}

	sdb, err := Connect(u.Driver, u.DSN)
	if err != nil {
		return nil, fmt.Errorf("unable to connect to database: %w", err)
	}

	// Apply supplies defaults; pass explicit non-positive values through to database/sql.
	sdb.SetMaxOpenConns(o.maxOpenConns)
	sdb.SetMaxIdleConns(o.maxIdleConns)
	sdb.SetConnMaxLifetime(o.connMaxLifetime)
	sdb.SetConnMaxIdleTime(o.connMaxIdleTime)

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

	keyName := m.KeyName()
	for i := range len(names) {
		if names[i] != keyName {
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
