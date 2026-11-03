// Package where provides chainable query conditions, pagination and sort
// options, applied to a *gorm.DB in one shot via Where.
// Package where 提供链式的查询条件、分页与排序选项，通过 Where 一次性应用到 *gorm.DB。
//
// 分页规则委托给 page 包；排序只接受 Sortable 白名单里声明过的字段。
package where

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/apus-run/gala/components/db/page"
)

const (
	// defaultLimit defines the default limit for pagination | defaultLimit 定义分页的默认条数。
	defaultLimit = -1
)

// ErrEmptyTenant is returned when the registered tenant value function returns
// an empty string.
// ErrEmptyTenant 表示已注册的租户取值函数返回了空字符串。
var ErrEmptyTenant = errors.New("where: tenant value is empty")

// ErrInvalidFilter is returned when F receives arguments that are not key/value
// pairs.
// ErrInvalidFilter 表示 F 的参数不是成对的 key/value。
var ErrInvalidFilter = errors.New("where: invalid filter")

// Tenant represents a tenant with a key and a function to retrieve its value | Tenant 表示一个租户：一个键和一个取值函数。
type Tenant struct {
	Key       string                           // Key associated with the tenant | 租户关联的键。
	ValueFunc func(ctx context.Context) string // Function that retrieves the tenant's value from the context | 根据ctx取该租户值的函数。
}

var (
	// tenant holds the registered tenant instance | tenant 保存已注册的租户实例。
	registeredTenant Tenant
	// Mutex protecting access to the registered tenant | 保护已注册租户访问的互斥锁。
	tenantMutex sync.RWMutex
)

// Where defines an interface for types that can modify GORM database queries | Where 定义可以修改 GORM 查询的类型的接口。
type Where interface {
	Where(db *gorm.DB) *gorm.DB
}

// Query represents a database query with its arguments.
// It contains the query condition and any associated parameters.
// Query 表示一条数据库查询及其参数，包含查询条件与关联的占位参数。
type Query struct {
	// Condition used in the GORM Where clause; can be a string, map, struct or any other type gorm supports | GORM Where 子句使用的条件：字符串、map、struct 或 gorm 支持的其他类型。
	Query any

	// Arguments replacing placeholders in the query condition | 替换查询条件中占位符的参数。
	Args []any
}

// Option defines a function type that modifies Options | Option 定义修改 Options 的选项类型。
type Option interface {
	apply(*Options)
}

// OptionFunc adapts a plain function to Option | OptionFunc 把普通函数适配为 Option。
type OptionFunc func(*Options)

func (f OptionFunc) apply(o *Options) {
	f(o)
}

// Options holds the options for GORM's Where query conditions | Options 保存 GORM Where 查询条件的选项。
type Options struct {
	// Starting point for pagination | 分页的起始偏移。
	// +optional
	Offset int `json:"offset"`
	// Maximum number of results to return | 返回结果的最大条数。
	// +optional
	Limit int `json:"limit"`
	// Key-value pairs for filtering records | 过滤记录的键值对。
	Filters map[any]any
	// Custom clauses appended to the query | 追加到查询的自定义子句。
	Clauses []clause.Expression
	// List of query conditions to be executed | 待执行的查询条件列表。
	Queries []Query
	// ORDER BY columns, usually produced by Sortable.Parse | ORDER BY 列，通常由 Sortable.Parse 生成。
	// +optional
	Orders []clause.OrderByColumn

	// err records parameter errors made while building (currently only F).
	// Chainable methods have no error return, so the error is held until Where
	// attaches it to the query and it surfaces with the execution result.
	// err 记录构造过程中的参数错误（目前只有 F）。链式方法没有 error 返回值，
	// 错误留到 Where 时挂到查询上，随执行结果返回。
	err error
}

// DefaultOptions returns a new Options with default values | DefaultOptions 返回带默认值的新 Options。
func DefaultOptions() *Options {
	return &Options{
		Offset:  0,
		Limit:   defaultLimit,
		Filters: map[any]any{},
		Clauses: make([]clause.Expression, 0),
	}
}

// NewOptions starts from DefaultOptions and applies opts in order | NewOptions 从 DefaultOptions 出发，按顺序应用 opts。
func NewOptions(opts ...Option) *Options {
	o := DefaultOptions()
	o.Apply(opts...)
	return o
}

// Apply applies opts in place in order, without resetting defaults or validating | Apply 按顺序原地应用 opts，不重置默认值、不做校验。
func (o *Options) Apply(opts ...Option) {
	for _, opt := range opts {
		opt.apply(o)
	}
}

// NewWhere constructs a new Options object, applying the given where options | NewWhere 构造一个新的 Options，并应用传入的 where 选项。
func NewWhere(opts ...Option) *Options {
	whr := NewOptions(opts...)

	return whr
}

// WithOffset initializes the Offset field in Options with the given offset value | WithOffset 用给定的 offset 值初始化 Options 的 Offset 字段。
func WithOffset(offset int64) Option {
	return OptionFunc(func(whr *Options) {
		if offset < 0 {
			offset = 0
		}
		whr.Offset = int(offset)
	})
}

// WithLimit initializes the Limit field in Options with the given limit value | WithLimit 用给定的 limit 值初始化 Options 的 Limit 字段。
func WithLimit(limit int64) Option {
	return OptionFunc(func(whr *Options) {
		if limit <= 0 {
			limit = defaultLimit
		}
		whr.Limit = int(limit)
	})
}

// WithPage is a sugar function to convert page and pageSize into limit and offset in Options.
//
// Normalization is delegated to page.Params, so WithPage and P always agree:
// pg<1 is page 1, size<1 is page.DefaultSize, size>page.MaxSize is capped.
// WithPage 是把 page 和 pageSize 换算成 Options 里 limit/offset 的语法糖。
//
// 归一委托给 page.Params，因此 WithPage 与 P 永远一致：
// pg<1 按第 1 页，size<1 取 page.DefaultSize，size>page.MaxSize 截断到上限。
func WithPage(pg int, size int) Option {
	return OptionFunc(func(whr *Options) {
		whr.P(pg, size)
	})
}

// WithFilter initializes the Filters field in Options with the given filter criteria | WithFilter 用给定的过滤条件初始化 Options 的 Filters 字段。
func WithFilter(filter map[any]any) Option {
	return OptionFunc(func(whr *Options) {
		whr.Filters = filter
	})
}

// WithClauses appends clauses to the Clauses field in Options | WithClauses 往 Options 的 Clauses 字段追加子句。
func WithClauses(conds ...clause.Expression) Option {
	return OptionFunc(func(whr *Options) {
		whr.Clauses = append(whr.Clauses, conds...)
	})
}

// WithQuery creates an Option that adds a query condition with arguments to the Options struct.
// The query parameter can be a string, map, struct, or any other type supported by GORM's Where clause.
// The args parameter contains values that will replace placeholders in the query string.
// WithQuery 创建一个 Option，向 Options 添加带参数的查询条件。
// query 参数可以是字符串、map、struct 或 GORM Where 子句支持的其他类型；
// args 参数的值会替换查询串里的占位符。
func WithQuery(query any, args ...any) Option {
	return OptionFunc(func(whr *Options) {
		whr.Queries = append(whr.Queries, Query{Query: query, Args: args})
	})
}

// WithOptions sets the whole configuration via a callback | WithOptions 通过回调设置所有配置。
func WithOptions(fn func(options *Options)) Option {
	return OptionFunc(func(options *Options) {
		fn(options)
	})
}

// O sets the offset for the query | O 设置查询的 offset。
func (whr *Options) O(offset int) *Options {
	if offset < 0 {
		offset = 0
	}
	whr.Offset = offset
	return whr
}

// L sets the limit for the query | L 设置查询的 limit。
func (whr *Options) L(limit int) *Options {
	if limit <= 0 {
		limit = defaultLimit // Negative means no limit | 负数表示不限制
	}
	whr.Limit = limit
	return whr
}

// P sets the pagination based on the page number and page size.
//
// The rules live in page.Params: pg<1 becomes 1, size<1 becomes page.DefaultSize,
// size>page.MaxSize is capped. Use WithLimit(0) for an unbounded query.
// P 按页码与每页条数设置分页。
//
// 规则在 page.Params 里：pg<1 归一为 1，size<1 取 page.DefaultSize，
// size>page.MaxSize 截断到上限。要不限条数地查询就用 WithLimit(0)。
func (whr *Options) P(pg int, size int) *Options {
	params := page.Params{Page: pg, Size: size}
	whr.Offset, whr.Limit = params.Offset(), params.Limit()
	return whr
}

// S appends ORDER BY columns, typically the result of Sortable.Parse | S 追加 ORDER BY 列，通常来自 Sortable.Parse。
func (whr *Options) S(columns ...clause.OrderByColumn) *Options {
	whr.Orders = append(whr.Orders, columns...)
	return whr
}

// C adds conditions to the query | C 为查询添加条件子句。
func (whr *Options) C(conds ...clause.Expression) *Options {
	whr.Clauses = append(whr.Clauses, conds...)
	return whr
}

// Q adds a query condition with arguments to the Options struct and returns the modified Options.
// This method appends a new Query instance to the Queries slice.
// Q 向 Options 添加带参数的查询条件并返回修改后的 Options。
// 该方法会往 Queries 切片追加一个新的 Query 实例。
func (whr *Options) Q(query any, args ...any) *Options {
	whr.Queries = append(whr.Queries, Query{Query: query, Args: args})
	return whr
}

// T retrieves the value associated with the registered tenant using the provided context | T 用传入的 ctx 取已注册租户对应的值。
func (whr *Options) T(ctx context.Context) *Options {
	tenantMutex.RLock()
	defer tenantMutex.RUnlock()

	if registeredTenant.Key != "" && registeredTenant.ValueFunc != nil {
		v := registeredTenant.ValueFunc(ctx)
		if v == "" {
			// When the tenant cannot be resolved (e.g. an anonymous request), tenant = ''
			// must not be generated: it would match rows of the empty tenant, or silently
			// return an empty set when the table has no empty tenant — both hide "this
			// request has no tenant". Like unpaired F arguments, record it as an error so
			// the query does not run.
			// 取不到租户（如匿名请求）时不能生成 tenant = ''：那会匹配空租户行，或在库里没有空租户时
			// 悄悄返回空集，两种都掩盖了「这次请求没有租户」。和 F 参数不成对一样，记成错误让查询不执行。
			if whr.err == nil {
				whr.err = ErrEmptyTenant
			}
			return whr
		}
		whr.F(registeredTenant.Key, v)
	}
	return whr
}

// F adds filters to the query.
//
// kvs must come in pairs. With an odd number of arguments, no filter is added
// and ErrInvalidFilter is recorded: that error is returned as a plain error
// when the query executes (List/Get/Count/Delete and friends) and the query
// does not run. It must not be silently ignored: if the dropped filter was the
// tenant filter, the query would return rows it should not; nor should it
// panic: a bad argument should not crash the whole process.
// F 为查询添加过滤条件。
//
// kvs 必须成对出现。奇数个参数时不添加任何过滤条件，并记录 ErrInvalidFilter：
// 该错误会在查询执行时（List/Get/Count/Delete 等）作为普通 error 返回，查询不会执行。
// 不能静默忽略：丢掉的若是租户过滤，查询会返回不该返回的行；也不 panic：参数错误
// 不该让整个进程崩溃。
func (whr *Options) F(kvs ...any) *Options {
	if len(kvs)%2 != 0 {
		if whr.err == nil {
			whr.err = fmt.Errorf("%w: need key/value pairs, got %d arguments", ErrInvalidFilter, len(kvs))
		}
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
// Where 把过滤、子句、排序、offset 和 limit 应用到给定的 gorm.DB。
//
// 它从不修改接收者，因此一份 Options 可以构造多条查询
// （store.List 需要从同一份 Options 构造计数查询与取页查询）。
// nil 接收者是空操作：(*Options)(nil) 会经由 Where 接口走到这里，
// 常规的 nil 检查拦不住它。
func (whr *Options) Where(db *gorm.DB) *gorm.DB {
	if whr == nil {
		return db
	}
	if whr.err != nil {
		// Session first, into an independent instance: the incoming db may be shared,
		// and AddError on it directly would pollute it.
		// 先 Session 成独立实例：传入的 db 可能是共享的，直接 AddError 会污染它。
		db = db.Session(&gorm.Session{})
		_ = db.AddError(whr.err)
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

// O is a convenience function to create a new Options with offset | O 是创建带 offset 的新 Options 的便捷函数。
func O(offset int) *Options {
	return NewWhere().O(offset)
}

// L is a convenience function to create a new Options with limit | L 是创建带 limit 的新 Options 的便捷函数。
func L(limit int) *Options {
	return NewWhere().L(limit)
}

// P is a convenience function to create a new Options with page number and page size | P 是创建带页码与每页条数的新 Options 的便捷函数。
func P(pg int, size int) *Options {
	return NewWhere().P(pg, size)
}

// C is a convenience function to create a new Options with conditions | C 是创建带条件子句的新 Options 的便捷函数。
func C(conds ...clause.Expression) *Options {
	return NewWhere().C(conds...)
}

// T is a convenience function to create a new Options with tenant.
// It behaves like (*Options).T: no filter is added when no tenant is registered.
// T 是创建带租户过滤的新 Options 的便捷函数。
// 与 (*Options).T 行为一致：未注册租户时不添加过滤条件。
func T(ctx context.Context) *Options {
	return NewWhere().T(ctx)
}

// F is a convenience function to create a new Options with filters | F 是创建带过滤条件的新 Options 的便捷函数。
func F(kvs ...any) *Options {
	return NewWhere().F(kvs...)
}

// RegisterTenant registers a new tenant with the specified key and value function | RegisterTenant 用给定的键和取值函数注册一个租户。
func RegisterTenant(key string, valueFunc func(context.Context) string) {
	tenantMutex.Lock()
	defer tenantMutex.Unlock()

	registeredTenant = Tenant{
		Key:       key,
		ValueFunc: valueFunc,
	}
}
