// Package store provides a generic data store implementation | Package store 提供通用的数据存取实现。
package store

import (
	"context"
	"errors"
	"regexp"

	"gorm.io/gorm"

	"github.com/apus-run/gala/components/db"
	"github.com/apus-run/gala/components/db/page"
	"github.com/apus-run/gala/components/db/where"
)

// ErrInvalidColumn is returned when the column name is not a valid identifier | ErrInvalidColumn 表示列名不是合法的标识符。
var ErrInvalidColumn = errors.New("store: invalid column name")

// validColumn matches `name` or `table.name` | validColumn 匹配 `name` 或 `table.name`。
var validColumn = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)?$`)

// Store represents a generic data store.
//
// Under a ctx passed in by db.Provider.TX, Store automatically lands on that
// transaction.
// Store 表示一个通用的数据存取器。
//
// Store 在 db.Provider.TX 传入的 ctx 下自动落在该事务上。
type Store[T any] struct {
	storage db.Provider
}

// NewStore creates a new instance of Store with the provided db.Provider | NewStore 用给定的 db.Provider 创建一个新的 Store 实例。
func NewStore[T any](storage db.Provider) *Store[T] {
	return &Store[T]{
		storage: storage,
	}
}

// db retrieves the database session and applies the provided where conditions | db 取数据库会话并应用传入的 where 条件。
func (s *Store[T]) db(ctx context.Context, wheres ...where.Where) *gorm.DB {
	session := s.storage.DB(ctx)
	for _, whr := range wheres {
		if whr != nil {
			session = whr.Where(session)
		}
	}
	return session
}

// Create inserts a new object into the database | Create 往数据库插入一个新对象。
func (s *Store[T]) Create(ctx context.Context, obj *T) error {
	if err := s.db(ctx).Create(obj).Error; err != nil {
		return err
	}
	return nil
}

// Update modifies an existing object in the database.
//
// It only updates rows that already exist: when the primary key is missing it
// returns gorm.ErrRecordNotFound instead of silently inserting a new row like
// Save would (which would let the caller fabricate a record out of someone
// else's, or a nonexistent, primary key). Zero-valued fields are written too.
// Update carries no where condition: to bound the scope (tenant, ownership),
// fetch the object with Get(ctx, opts) first and then update it.
// Update 修改数据库中一个已存在的对象。
//
// 只更新已存在的行：主键不存在时返回 gorm.ErrRecordNotFound，不会像 Save 那样悄悄插入新行
// （那会让调用方拿一个别人的、或不存在的主键凭空造出一条记录）。零值字段也会写入。
// Update 不带 where 条件：需要限制范围（租户、归属）时先用 Get(ctx, opts) 取出对象再更新。
func (s *Store[T]) Update(ctx context.Context, obj *T) error {
	res := s.db(ctx).Select("*").Updates(obj)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// Delete removes objects from the database based on the provided where options | Delete 按传入的 where 条件从数据库删除对象。
func (s *Store[T]) Delete(ctx context.Context, opts *where.Options) error {
	err := s.db(ctx, opts).Delete(new(T)).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return nil
}

// Get retrieves a single object from the database based on the provided where options.
// On a miss it returns gorm.ErrRecordNotFound; the caller checks with errors.Is.
//
// A nil opts means no conditions at all and returns the first row of the table.
// In multi-tenant scenarios you must pass where.T(ctx), or your own ownership
// condition; when building opts from a request, check first that it is not nil.
// Get 按传入的 where 条件从数据库取单个对象。
// 未命中时返回 gorm.ErrRecordNotFound，调用方用 errors.Is 判断。
//
// opts 为 nil 表示不加任何条件，返回表里的第一行。多租户场景下必须传 where.T(ctx)，
// 或自己的归属条件；从请求构造 opts 时先检查它不是 nil。
func (s *Store[T]) Get(ctx context.Context, opts *where.Options) (*T, error) {
	var obj T
	if err := s.db(ctx, opts).First(&obj).Error; err != nil {
		return nil, err
	}
	return &obj, nil
}

// List retrieves a list of objects from the database based on the provided where options.
//
// The count query and the page query are built separately from the same Options.
// where.Options.Where does not mutate its receiver, so filters and pagination are
// applied once per query. Without Orders the default stays id desc.
//
// When no size is given (opts is nil or Limit<=0), only page.DefaultSize rows are
// fetched to avoid reading the whole table into memory at once; the returned
// count is still the total number of matches. Ask for more with where.P or
// where.L explicitly.
// List 按传入的 where 条件从数据库取一组对象。
//
// 计数查询与取页查询从同一份 Options 分别构造。where.Options.Where 不会改动接收者，
// 因此过滤与分页在每条查询里各应用一次。不带 Orders 时默认仍按 id desc 排序。
//
// 没有指定条数（opts 为 nil 或 Limit<=0）时只取 page.DefaultSize 条，避免一次把整张表
// 读进内存；返回的 count 仍是全量匹配数。需要更多时用 where.P 或 where.L 显式指定。
func (s *Store[T]) List(ctx context.Context, opts *where.Options) (count int64, rets []*T, err error) {
	if err = s.db(ctx, opts).Model(new(T)).Offset(-1).Limit(-1).Count(&count).Error; err != nil {
		return 0, nil, err
	}
	if count == 0 {
		return 0, []*T{}, nil
	}

	q := s.db(ctx, opts)
	if opts == nil || opts.Limit <= 0 {
		q = q.Limit(page.DefaultSize)
	}
	if opts == nil || len(opts.Orders) == 0 {
		q = q.Order("id desc")
	}
	if err = q.Find(&rets).Error; err != nil {
		return 0, nil, err
	}
	return count, rets, nil
}

// Count returns the number of objects in the database that match the provided where options.
// Pagination in opts is ignored: like List it returns the total number of
// matches, not the row count after LIMIT/OFFSET.
// Count 返回数据库中匹配传入 where 条件的对象数。
// opts 里的分页被忽略：和 List 一样返回全量匹配数，而不是 LIMIT/OFFSET 之后的行数。
func (s *Store[T]) Count(ctx context.Context, opts *where.Options) (count int64, err error) {
	err = s.db(ctx, opts).Model(new(T)).Offset(-1).Limit(-1).Count(&count).Error
	if err != nil {
		return 0, err
	}
	return count, nil
}

// Pluck retrieves a list of values for a specific column from the database based on the provided where options.
//
// column must be a single column name (optionally table-prefixed): it is handed
// to gorm as a raw SQL fragment; strings containing spaces or punctuation are
// not quoted, so once a request-supplied field reaches here it is SQL injection.
// Pluck 按传入的 where 条件从数据库取某一列的值列表。
//
// column 必须是单个列名（可带表名前缀）：它会作为原始 SQL 片段交给 gorm，带空格或标点的
// 字符串不会被加引号，调用方一旦把请求字段传进来就是 SQL 注入。
func (s *Store[T]) Pluck(ctx context.Context, column string, opts *where.Options) (rets []any, err error) {
	if !validColumn.MatchString(column) {
		return nil, ErrInvalidColumn
	}
	err = s.db(ctx, opts).Model(new(T)).Pluck(column, &rets).Error
	if err != nil {
		return nil, err
	}
	return rets, nil
}
