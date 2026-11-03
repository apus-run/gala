package repo

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/apus-run/gala/components/db/page"
)

var (
	// ErrRecordNotFound is returned when a record is not found | ErrRecordNotFound 找不到记录时返回。
	ErrRecordNotFound = errors.New("record not found")
	// ErrInvalidID is returned when an invalid ID is provided | ErrInvalidID 提供了非法 ID 时返回。
	ErrInvalidID = errors.New("invalid id")
	// ErrDuplicateRecord is returned when trying to create a duplicate record | ErrDuplicateRecord 试图创建重复记录时返回。
	ErrDuplicateRecord = errors.New("duplicate record")
)

// Repository defines the standard repository interface | Repository 定义标准仓储接口。
type Repository[T any] interface {
	// Basic CRUD operations | 基础 CRUD 操作。
	FindByID(ctx context.Context, id any) (*T, error)
	FindOrFail(ctx context.Context, id any) (*T, error)
	First(ctx context.Context) (*T, error)
	FirstOrFail(ctx context.Context) (*T, error)
	FindAll(ctx context.Context) ([]T, error)
	Create(ctx context.Context, data *T) error
	Update(ctx context.Context, id any, data *T) error
	Delete(ctx context.Context, id any) error

	// Query building | 查询构造。
	Where(field string, value any) Query[T]
	WhereIn(field string, values []any) Query[T]
	WhereNotIn(field string, values []any) Query[T]
	WhereBetween(field string, start, end any) Query[T]
	WhereNull(field string) Query[T]
	WhereNotNull(field string) Query[T]
	With(relation string) Query[T]
	OrderBy(field string, direction string) Query[T]
	Limit(limit int) Query[T]
	Offset(offset int) Query[T]

	// Utility methods | 工具方法。
	Exists(ctx context.Context) (bool, error)
	DoesNotExist(ctx context.Context) (bool, error)
	Count(ctx context.Context) (int64, error)
	Pluck(ctx context.Context, field string) ([]any, error)
	PluckString(ctx context.Context, field string) ([]string, error)
	PluckInt(ctx context.Context, field string) ([]int, error)

	// Bulk operations | 批量操作。
	CreateBatch(ctx context.Context, data []T) error
	UpdateBatch(ctx context.Context, ids []any, data []T) error
	DeleteBatch(ctx context.Context, ids []any) error

	// Atomic operations | 原子操作。
	Increment(ctx context.Context, id any, field string, value int) error
	Decrement(ctx context.Context, id any, field string, value int) error

	// Transaction support | 事务支持。
	WithTransaction(tx *gorm.DB) Repository[T]
}

// Query represents a chainable query builder | Query 表示可链式调用的查询构造器。
type Query[T any] interface {
	// Additional conditions | 附加条件。
	Where(field string, value any) Query[T]
	WhereIn(field string, values []any) Query[T]
	WhereNotIn(field string, values []any) Query[T]
	WhereBetween(field string, start, end any) Query[T]
	WhereNull(field string) Query[T]
	WhereNotNull(field string) Query[T]
	OrWhere(field string, value any) Query[T]

	// Relationships | 关联。
	With(relation string) Query[T]

	// Ordering and limiting | 排序与条数限制。
	OrderBy(field string, direction string) Query[T]
	OrderByDesc(field string) Query[T]
	OrderByAsc(field string) Query[T]
	Limit(limit int) Query[T]
	Offset(offset int) Query[T]

	// Grouping | 分组。
	GroupBy(fields ...string) Query[T]
	Having(condition string, value any) Query[T]

	// Selection | 字段选择。
	Select(fields ...string) Query[T]
	Distinct() Query[T]

	// Execution | 执行。
	Find(ctx context.Context) ([]T, error)
	First(ctx context.Context) (*T, error)
	FirstOrFail(ctx context.Context) (*T, error)
	Exists(ctx context.Context) (bool, error)
	DoesNotExist(ctx context.Context) (bool, error)
	Count(ctx context.Context) (int64, error)
	Pluck(ctx context.Context, field string) ([]any, error)
	Delete(ctx context.Context) error
	Update(ctx context.Context, data map[string]any) error

	// Pagination | 分页。
	Paginate(page, perPage int) PaginatedResult[T]
}

// PaginatedResult represents a paginated query result | PaginatedResult 表示分页查询结果。
type PaginatedResult[T any] interface {
	// Execute runs the query and returns the metadata with one page of rows | Execute 执行查询，返回元数据与一页数据。
	Execute(ctx context.Context) (*PaginationMeta, []T, error)
}

// PaginationMeta contains pagination metadata.
//
// It is an alias of page.Meta, so the JSON shape and the existing call sites
// are unchanged while the normalization rules live in exactly one place.
// PaginationMeta 保存分页元数据。
//
// 它是 page.Meta 的别名：JSON 形状与既有调用点不变，归一规则只在一处维护。
type PaginationMeta = page.Meta

// BaseRepository provides common functionality for all repositories | BaseRepository 为所有仓储提供通用功能。
type BaseRepository[T any] interface {
	Repository[T]

	// Database-specific methods | 数据库特有方法。
	Raw(ctx context.Context, query string, args ...any) ([]T, error)
	Exec(ctx context.Context, query string, args ...any) error

	// Hooks | 钩子。
	BeforeCreate(ctx context.Context, data *T) error
	AfterCreate(ctx context.Context, data *T) error
	BeforeUpdate(ctx context.Context, id any, data *T) error
	AfterUpdate(ctx context.Context, id any, data *T) error
	BeforeDelete(ctx context.Context, id any) error
	AfterDelete(ctx context.Context, id any) error
}
