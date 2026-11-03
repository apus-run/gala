// Package repo provides a generic repository and fluent query abstraction
// over MongoDB | repo 提供 MongoDB 之上的泛型仓库与链式查询抽象.
//
// # Notes | 说明
//
// MongoQuery implements the fluent Query interface, but several SQL-flavored
// methods are no-ops on MongoDB: With, WithCount, GroupBy, Having and
// Distinct accept the call and change nothing. WithTransaction panics unless
// the argument is a mongo.SessionContext, and transactions require a replica
// set. IDs are ObjectIDs: string ids are parsed as hex.
// MongoQuery 实现了链式 Query 接口，但若干 SQL 风格方法在 MongoDB 上是
// no-op：With、WithCount、GroupBy、Having 与 Distinct 调用后不产生任何效果。
// WithTransaction 在参数不是 mongo.SessionContext 时 panic，事务需要副本集。
// ID 均为 ObjectID：字符串 id 按十六进制解析。
package repo

import (
	"context"
	"errors"
)

var (
	// ErrRecordNotFound is returned when a record is not found
	// | ErrRecordNotFound 在记录不存在时返回。
	ErrRecordNotFound = errors.New("record not found")
	// ErrInvalidID is returned when an invalid ID is provided
	// | ErrInvalidID 在传入非法 ID 时返回。
	ErrInvalidID = errors.New("invalid id")
	// ErrDuplicateRecord is returned when trying to create a duplicate record
	// | ErrDuplicateRecord 在创建重复记录时返回。
	ErrDuplicateRecord = errors.New("duplicate record")
)

// Repository defines the standard repository interface
// | Repository 定义标准仓库接口。
type Repository[T any] interface {
	// Basic CRUD operations | 基础 CRUD 操作
	FindByID(ctx context.Context, id any) (*T, error)
	FindOrFail(ctx context.Context, id any) (*T, error)
	First(ctx context.Context) (*T, error)
	FirstOrFail(ctx context.Context) (*T, error)
	FindAll(ctx context.Context) ([]T, error)
	Create(ctx context.Context, data *T) error
	Update(ctx context.Context, id any, data *T) error
	Delete(ctx context.Context, id any) error

	// Query building | 查询构造
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

	// Utility methods | 工具方法
	Exists(ctx context.Context) (bool, error)
	DoesNotExist(ctx context.Context) (bool, error)
	Count(ctx context.Context) (int64, error)
	Pluck(ctx context.Context, field string) ([]any, error)
	PluckString(ctx context.Context, field string) ([]string, error)
	PluckInt(ctx context.Context, field string) ([]int, error)

	// Bulk operations | 批量操作
	CreateBatch(ctx context.Context, data []T) error
	UpdateBatch(ctx context.Context, ids []any, data []T) error
	DeleteBatch(ctx context.Context, ids []any) error

	// Atomic operations | 原子操作
	Increment(ctx context.Context, id any, field string, value int) error
	Decrement(ctx context.Context, id any, field string, value int) error

	// Transaction support | 事务支持
	WithTransaction(tx any) Repository[T]
}

// Query represents a chainable query builder | Query 表示可链式调用的查询构造器。
type Query[T any] interface {
	// Additional conditions | 附加条件
	Where(field string, value any) Query[T]
	WhereIn(field string, values []any) Query[T]
	WhereNotIn(field string, values []any) Query[T]
	WhereBetween(field string, start, end any) Query[T]
	WhereNull(field string) Query[T]
	WhereNotNull(field string) Query[T]
	OrWhere(field string, value any) Query[T]

	// Relationships | 关联（MongoDB 上为 no-op）
	With(relation string) Query[T]
	WithCount(relation string) Query[T]

	// Ordering and limiting | 排序与分页截取
	OrderBy(field string, direction string) Query[T]
	OrderByDesc(field string) Query[T]
	OrderByAsc(field string) Query[T]
	Limit(limit int) Query[T]
	Offset(offset int) Query[T]

	// Grouping | 分组（MongoDB 上为 no-op）
	GroupBy(fields ...string) Query[T]
	Having(condition string, value any) Query[T]

	// Selection | 字段选择
	Select(fields ...string) Query[T]
	Distinct() Query[T]

	// Execution | 执行
	Find(ctx context.Context) ([]T, error)
	First(ctx context.Context) (*T, error)
	FirstOrFail(ctx context.Context) (*T, error)
	Exists(ctx context.Context) (bool, error)
	DoesNotExist(ctx context.Context) (bool, error)
	Count(ctx context.Context) (int64, error)
	Pluck(ctx context.Context, field string) ([]any, error)
	Delete(ctx context.Context) error
	Update(ctx context.Context, data map[string]any) error

	// Pagination | 分页
	Paginate(page, perPage int) PaginatedResult[T]
}

// PaginatedResult represents a paginated query result
// | PaginatedResult 表示分页查询结果。
type PaginatedResult[T any] interface {
	Execute(ctx context.Context) (*PaginationMeta, []T, error)
}

// PaginationMeta contains pagination metadata | PaginationMeta 持有分页元数据。
type PaginationMeta struct {
	// Page is the current page number (1-based) | 当前页码（从 1 开始）
	Page int `json:"page"`
	// PerPage is the page size | 每页条数
	PerPage int `json:"per_page"`
	// Total is the total record count | 总记录数
	Total int64 `json:"total"`
	// TotalPages is the total page count | 总页数
	TotalPages int `json:"total_pages"`
	// HasNext reports whether a next page exists | 是否存在下一页
	HasNext bool `json:"has_next"`
	// HasPrev reports whether a previous page exists | 是否存在上一页
	HasPrev bool `json:"has_prev"`
}

// BaseRepository provides common functionality for all repositories
// | BaseRepository 为所有仓库提供公共能力。
type BaseRepository[T any] interface {
	Repository[T]

	// Database-specific methods | 数据库特定方法
	Raw(ctx context.Context, query string, args ...any) ([]T, error)
	Exec(ctx context.Context, query string, args ...any) error

	// Hooks | 钩子
	BeforeCreate(ctx context.Context, data *T) error
	AfterCreate(ctx context.Context, data *T) error
	BeforeUpdate(ctx context.Context, id any, data *T) error
	AfterUpdate(ctx context.Context, id any, data *T) error
	BeforeDelete(ctx context.Context, id any) error
	AfterDelete(ctx context.Context, id any) error
}
