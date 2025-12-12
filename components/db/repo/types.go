package repo

import (
	"context"
	"errors"
)

var (
	// ErrRecordNotFound is returned when a record is not found
	ErrRecordNotFound = errors.New("record not found")
	// ErrInvalidID is returned when an invalid ID is provided
	ErrInvalidID = errors.New("invalid id")
	// ErrDuplicateRecord is returned when trying to create a duplicate record
	ErrDuplicateRecord = errors.New("duplicate record")
)

// Repository defines the standard repository interface
type Repository[T any] interface {
	// Basic CRUD operations
	FindByID(ctx context.Context, id any) (*T, error)
	FindOrFail(ctx context.Context, id any) (*T, error)
	First(ctx context.Context) (*T, error)
	FirstOrFail(ctx context.Context) (*T, error)
	FindAll(ctx context.Context) ([]T, error)
	Create(ctx context.Context, data *T) error
	Update(ctx context.Context, id any, data *T) error
	Delete(ctx context.Context, id any) error

	// Query building
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

	// Utility methods
	Exists(ctx context.Context) (bool, error)
	DoesNotExist(ctx context.Context) (bool, error)
	Count(ctx context.Context) (int64, error)
	Pluck(ctx context.Context, field string) ([]any, error)
	PluckString(ctx context.Context, field string) ([]string, error)
	PluckInt(ctx context.Context, field string) ([]int, error)

	// Bulk operations
	CreateBatch(ctx context.Context, data []T) error
	UpdateBatch(ctx context.Context, ids []any, data []T) error
	DeleteBatch(ctx context.Context, ids []any) error

	// Atomic operations
	Increment(ctx context.Context, id any, field string, value int) error
	Decrement(ctx context.Context, id any, field string, value int) error

	// Transaction support
	WithTransaction(tx any) Repository[T]
}

// Query represents a chainable query builder
type Query[T any] interface {
	// Additional conditions
	Where(field string, value any) Query[T]
	WhereIn(field string, values []any) Query[T]
	WhereNotIn(field string, values []any) Query[T]
	WhereBetween(field string, start, end any) Query[T]
	WhereNull(field string) Query[T]
	WhereNotNull(field string) Query[T]
	OrWhere(field string, value any) Query[T]

	// Relationships
	With(relation string) Query[T]
	WithCount(relation string) Query[T]

	// Ordering and limiting
	OrderBy(field string, direction string) Query[T]
	OrderByDesc(field string) Query[T]
	OrderByAsc(field string) Query[T]
	Limit(limit int) Query[T]
	Offset(offset int) Query[T]

	// Grouping
	GroupBy(fields ...string) Query[T]
	Having(condition string, value any) Query[T]

	// Selection
	Select(fields ...string) Query[T]
	Distinct() Query[T]

	// Execution
	Find(ctx context.Context) ([]T, error)
	First(ctx context.Context) (*T, error)
	FirstOrFail(ctx context.Context) (*T, error)
	Exists(ctx context.Context) (bool, error)
	DoesNotExist(ctx context.Context) (bool, error)
	Count(ctx context.Context) (int64, error)
	Pluck(ctx context.Context, field string) ([]any, error)
	Delete(ctx context.Context) error
	Update(ctx context.Context, data map[string]any) error

	// Pagination
	Paginate(page, perPage int) PaginatedResult[T]
}

// PaginatedResult represents a paginated query result
type PaginatedResult[T any] interface {
	Execute(ctx context.Context) (*PaginationMeta, []T, error)
}

// PaginationMeta contains pagination metadata
type PaginationMeta struct {
	Page       int   `json:"page"`
	PerPage    int   `json:"per_page"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
	HasNext    bool  `json:"has_next"`
	HasPrev    bool  `json:"has_prev"`
}

// BaseRepository provides common functionality for all repositories
type BaseRepository[T any] interface {
	Repository[T]

	// Database-specific methods
	Raw(ctx context.Context, query string, args ...any) ([]T, error)
	Exec(ctx context.Context, query string, args ...any) error

	// Hooks
	BeforeCreate(ctx context.Context, data *T) error
	AfterCreate(ctx context.Context, data *T) error
	BeforeUpdate(ctx context.Context, id any, data *T) error
	AfterUpdate(ctx context.Context, id any, data *T) error
	BeforeDelete(ctx context.Context, id any) error
	AfterDelete(ctx context.Context, id any) error
}
