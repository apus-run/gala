// Package where 提供链式的查询条件、分页与排序选项，通过 Where 一次性应用到 *gorm.DB。
//
// 分页规则委托给 page 包；排序只接受 Sortable 白名单里声明过的字段。
package where

import (
	"context"
	"sync"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/apus-run/gala/components/db/page"
)

const (
	// defaultLimit defines the default limit for pagination.
	defaultLimit = -1
)

// Tenant represents a tenant with a key and a function to retrieve its value.
type Tenant struct {
	Key       string                           // The key associated with the tenant
	ValueFunc func(ctx context.Context) string // Function to retrieve the tenant's value based on the context
}

var (
	// tenant holds the registered tenant instance.
	registeredTenant Tenant
	// Mutex to protect access to the registered tenant
	tenantMutex sync.RWMutex
)

// Where defines an interface for types that can modify GORM database queries.
type Where interface {
	Where(db *gorm.DB) *gorm.DB
}

// Query represents a database query with its arguments.
// It contains the query condition and any associated parameters.
type Query struct {
	// Query holds the condition to be used in the GORM query.
	// Can be a string, map, struct, or other types supported by GORM's Where clause.
	Query any

	// Args holds the arguments that will be passed to the query condition.
	// These values will be used to replace placeholders in the query.
	Args []any
}

// Option defines a function type that modifies Options.
type Option func(*Options)

// Options holds the options for GORM's Where query conditions.
type Options struct {
	// Offset defines the starting point for pagination.
	// +optional
	Offset int `json:"offset"`
	// Limit defines the maximum number of results to return.
	// +optional
	Limit int `json:"limit"`
	// Filters contains key-value pairs for filtering records.
	Filters map[any]any
	// Clauses contains custom clauses to be appended to the query.
	Clauses []clause.Expression
	// Queries contains a list of queries to be executed.
	Queries []Query
	// Orders defines the ORDER BY columns, usually produced by Sortable.Parse.
	// +optional
	Orders []clause.OrderByColumn
}

// DefaultOptions .
func DefaultOptions() *Options {
	return &Options{
		Offset:  0,
		Limit:   defaultLimit,
		Filters: map[any]any{},
		Clauses: make([]clause.Expression, 0),
	}
}

func Apply(opts ...Option) *Options {
	options := DefaultOptions()
	for _, opt := range opts {
		opt(options) // Apply each Option to the opts.
	}
	return options
}

// NewWhere constructs a new Options object, applying the given where options.
func NewWhere(opts ...Option) *Options {
	whr := Apply(opts...)

	return whr
}

// WithOffset initializes the Offset field in Options with the given offset value.
func WithOffset(offset int64) Option {
	return func(whr *Options) {
		if offset < 0 {
			offset = 0
		}
		whr.Offset = int(offset)
	}
}

// WithLimit initializes the Limit field in Options with the given limit value.
func WithLimit(limit int64) Option {
	return func(whr *Options) {
		if limit <= 0 {
			limit = defaultLimit
		}
		whr.Limit = int(limit)
	}
}

// WithPage is a sugar function to convert page and pageSize into limit and offset in Options.
//
// Normalization is delegated to page.Params, so WithPage and P always agree:
// pg<1 is page 1, size<1 is page.DefaultSize, size>page.MaxSize is capped.
func WithPage(pg int, size int) Option {
	return func(whr *Options) {
		whr.P(pg, size)
	}
}

// WithFilter initializes the Filters field in Options with the given filter criteria.
func WithFilter(filter map[any]any) Option {
	return func(whr *Options) {
		whr.Filters = filter
	}
}

// WithClauses appends clauses to the Clauses field in Options.
func WithClauses(conds ...clause.Expression) Option {
	return func(whr *Options) {
		whr.Clauses = append(whr.Clauses, conds...)
	}
}

// WithQuery creates an Option that adds a query condition with arguments to the Options struct.
// The query parameter can be a string, map, struct, or any other type supported by GORM's Where clause.
// The args parameter contains values that will replace placeholders in the query string.
func WithQuery(query any, args ...any) Option {
	return func(whr *Options) {
		whr.Queries = append(whr.Queries, Query{Query: query, Args: args})
	}
}

// WithOptions 设置所有配置
func WithOptions(fn func(options *Options)) Option {
	return func(options *Options) {
		fn(options)
	}
}

// O sets the offset for the query.
func (whr *Options) O(offset int) *Options {
	if offset < 0 {
		offset = 0
	}
	whr.Offset = offset
	return whr
}

// L sets the limit for the query.
func (whr *Options) L(limit int) *Options {
	if limit <= 0 {
		limit = defaultLimit // Ensure defaultLimit is defined elsewhere
	}
	whr.Limit = limit
	return whr
}

// P sets the pagination based on the page number and page size.
//
// The rules live in page.Params: pg<1 becomes 1, size<1 becomes page.DefaultSize,
// size>page.MaxSize is capped. Use WithLimit(0) for an unbounded query.
func (whr *Options) P(pg int, size int) *Options {
	params := page.Params{Page: pg, Size: size}
	whr.Offset, whr.Limit = params.Offset(), params.Limit()
	return whr
}

// S appends ORDER BY columns, typically the result of Sortable.Parse.
func (whr *Options) S(columns ...clause.OrderByColumn) *Options {
	whr.Orders = append(whr.Orders, columns...)
	return whr
}

// C adds conditions to the query.
func (whr *Options) C(conds ...clause.Expression) *Options {
	whr.Clauses = append(whr.Clauses, conds...)
	return whr
}

// Q adds a query condition with arguments to the Options struct and returns the modified Options.
// This method appends a new Query instance to the Queries slice.
func (whr *Options) Q(query any, args ...any) *Options {
	whr.Queries = append(whr.Queries, Query{Query: query, Args: args})
	return whr
}

// T retrieves the value associated with the registered tenant using the provided context.
func (whr *Options) T(ctx context.Context) *Options {
	tenantMutex.RLock()
	defer tenantMutex.RUnlock()

	if registeredTenant.Key != "" && registeredTenant.ValueFunc != nil {
		whr.F(registeredTenant.Key, registeredTenant.ValueFunc(ctx))
	}
	return whr
}

// F adds filters to the query.
func (whr *Options) F(kvs ...any) *Options {
	if len(kvs)%2 != 0 {
		// Handle error: uneven number of key-value pairs
		return whr
	}

	for i := 0; i < len(kvs); i += 2 {
		key := kvs[i]
		value := kvs[i+1]
		whr.Filters[key] = value
	}

	return whr
}

// Where applies filters, clauses, ordering, offset and limit to the given gorm.DB.
//
// It never modifies the receiver, so one Options can build several queries
// (store.List needs a count query and a page query from the same Options).
// A nil receiver is a no-op: (*Options)(nil) reaches here through the Where
// interface, where the usual nil check cannot catch it.
func (whr *Options) Where(db *gorm.DB) *gorm.DB {
	if whr == nil {
		return db
	}

	conditions := make([]clause.Expression, 0, len(whr.Clauses)+len(whr.Queries)+1)
	conditions = append(conditions, whr.Clauses...)
	for _, query := range whr.Queries {
		conditions = append(conditions, db.Statement.BuildCondition(query.Query, query.Args...)...)
	}
	if len(whr.Orders) > 0 {
		conditions = append(conditions, clause.OrderBy{Columns: whr.Orders})
	}

	return db.Where(whr.Filters).Clauses(conditions...).Offset(whr.Offset).Limit(whr.Limit)
}

// O is a convenience function to create a new Options with offset.
func O(offset int) *Options {
	return NewWhere().O(offset)
}

// L is a convenience function to create a new Options with limit.
func L(limit int) *Options {
	return NewWhere().L(limit)
}

// P is a convenience function to create a new Options with page number and page size.
func P(pg int, size int) *Options {
	return NewWhere().P(pg, size)
}

// C is a convenience function to create a new Options with conditions.
func C(conds ...clause.Expression) *Options {
	return NewWhere().C(conds...)
}

// T is a convenience function to create a new Options with tenant.
func T(ctx context.Context) *Options {
	return NewWhere().F(registeredTenant.Key, registeredTenant.ValueFunc(ctx))
}

// F is a convenience function to create a new Options with filters.
func F(kvs ...any) *Options {
	return NewWhere().F(kvs...)
}

// RegisterTenant registers a new tenant with the specified key and value function.
func RegisterTenant(key string, valueFunc func(context.Context) string) {
	tenantMutex.Lock()
	defer tenantMutex.Unlock()

	registeredTenant = Tenant{
		Key:       key,
		ValueFunc: valueFunc,
	}
}
