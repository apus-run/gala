package where

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/apus-run/gala/components/db/page"
)

func TestOptions_P(t *testing.T) {
	tests := []struct {
		name     string
		page     int
		pageSize int
		expected Options
	}{
		{
			name:     "Valid page and pageSize",
			page:     2,
			pageSize: 10,
			expected: Options{Offset: 10, Limit: 10},
		},
		{
			name:     "Page is zero",
			page:     0,
			pageSize: 10,
			expected: Options{Offset: 0, Limit: 10},
		},
		{
			name:     "PageSize is zero falls back to the default",
			page:     2,
			pageSize: 0,
			expected: Options{Offset: page.DefaultSize, Limit: page.DefaultSize},
		},
		{
			name:     "Negative page and pageSize",
			page:     -1,
			pageSize: -5,
			expected: Options{Offset: 0, Limit: page.DefaultSize},
		},
		{
			name:     "PageSize above the cap is clamped",
			page:     2,
			pageSize: page.MaxSize + 1,
			expected: Options{Offset: page.MaxSize, Limit: page.MaxSize},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options := &Options{}
			options.P(tt.page, tt.pageSize)

			if options.Offset != tt.expected.Offset || options.Limit != tt.expected.Limit {
				t.Errorf("Expected Offset: %d, Limit: %d, got Offset: %d, Limit: %d",
					tt.expected.Offset, tt.expected.Limit, options.Offset, options.Limit)
			}
		})
	}
}

func TestOptions_Q(t *testing.T) {
	tests := []struct {
		name     string
		query    any
		args     []any
		expected []Query
	}{
		{
			name:     "Single query with args",
			query:    "name = ?",
			args:     []any{"John"},
			expected: []Query{{Query: "name = ?", Args: []any{"John"}}},
		},
		{
			name:     "Multiple queries",
			query:    "age > ?",
			args:     []any{30},
			expected: []Query{{Query: "age > ?", Args: []any{30}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options := &Options{}
			options.Q(tt.query, tt.args...)

			if !reflect.DeepEqual(options.Queries, tt.expected) {
				t.Errorf("Expected Queries: %v, got: %v", tt.expected, options.Queries)
			}
		})
	}
}

func TestOptions_T(t *testing.T) {
	RegisterTenant("tenant_id", func(ctx context.Context) string {
		return "12345"
	})

	tests := []struct {
		name     string
		ctx      context.Context
		expected map[any]any
	}{
		{
			name:     "Tenant value added to filters",
			ctx:      context.Background(),
			expected: map[any]any{"tenant_id": "12345"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options := &Options{Filters: map[any]any{}}
			options.T(tt.ctx)

			if !reflect.DeepEqual(options.Filters, tt.expected) {
				t.Errorf("Expected Filters: %v, got: %v", tt.expected, options.Filters)
			}
		})
	}
}

func TestOptions_F(t *testing.T) {
	tests := []struct {
		name       string
		initial    map[any]any
		input      []any
		expected   map[any]any
		shouldSkip bool
	}{
		{
			name:     "Valid key-value pairs",
			initial:  map[any]any{},
			input:    []any{"key1", "value1", "key2", "value2"},
			expected: map[any]any{"key1": "value1", "key2": "value2"},
		},
		{
			name:     "Merge with existing filters",
			initial:  map[any]any{"existingKey": "existingValue"},
			input:    []any{"key1", "value1"},
			expected: map[any]any{"existingKey": "existingValue", "key1": "value1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options := &Options{
				Filters: tt.initial,
			}

			options.F(tt.input...)

			if tt.shouldSkip {
				if !reflect.DeepEqual(options.Filters, tt.initial) {
					t.Errorf("Expected filters to remain unchanged, got: %v", options.Filters)
				}
				return
			}

			if !reflect.DeepEqual(options.Filters, tt.expected) {
				t.Errorf("Expected filters: %v, got: %v", tt.expected, options.Filters)
			}
		})
	}
}

func TestOptions_O(t *testing.T) {
	tests := []struct {
		name     string
		offset   int
		expected Options
	}{
		{
			name:     "Valid offset",
			offset:   5,
			expected: Options{Offset: 5},
		},
		{
			name:     "Negative offset",
			offset:   -3,
			expected: Options{Offset: 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options := &Options{}
			options.O(tt.offset)

			if options.Offset != tt.expected.Offset || options.Limit != tt.expected.Limit {
				t.Errorf("Expected Offset: %d, Limit: %d, got Offset: %d, Limit: %d",
					tt.expected.Offset, tt.expected.Limit, options.Offset, options.Limit)
			}
		})
	}
}

func TestOptions_L(t *testing.T) {
	tests := []struct {
		name     string
		limit    int
		expected Options
	}{
		{
			name:     "Valid limit",
			limit:    10,
			expected: Options{Offset: 0, Limit: 10},
		},
		{
			name:     "Zero limit",
			limit:    0,
			expected: Options{Offset: 0, Limit: defaultLimit},
		},
		{
			name:     "Negative limit",
			limit:    -5,
			expected: Options{Offset: 0, Limit: defaultLimit},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options := &Options{}
			options.L(tt.limit)

			if options.Offset != tt.expected.Offset || options.Limit != tt.expected.Limit {
				t.Errorf("Expected Offset: %d, Limit: %d, got Offset: %d, Limit: %d",
					tt.expected.Offset, tt.expected.Limit, options.Offset, options.Limit)
			}
		})
	}
}

func TestOptions_WithOffset(t *testing.T) {
	tests := []struct {
		name     string
		offset   int64
		expected int
	}{
		{
			name:     "Valid offset",
			offset:   10,
			expected: 10,
		},
		{
			name:     "Negative offset",
			offset:   -5,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			option := WithOffset(tt.offset)
			options := &Options{}
			option(options)

			if options.Offset != tt.expected {
				t.Errorf("Expected Offset: %d, got: %d", tt.expected, options.Offset)
			}
		})
	}
}

func TestOptions_WithLimit(t *testing.T) {
	tests := []struct {
		name     string
		limit    int64
		expected int
	}{
		{
			name:     "Valid limit",
			limit:    20,
			expected: 20,
		},
		{
			name:     "Zero limit",
			limit:    0,
			expected: defaultLimit,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			option := WithLimit(tt.limit)
			options := &Options{}
			option(options)

			if options.Limit != tt.expected {
				t.Errorf("Expected Limit: %d, got: %d", tt.expected, options.Limit)
			}
		})
	}
}

func TestOptions_WithPage(t *testing.T) {
	tests := []struct {
		name     string
		page     int
		pageSize int
		expected Options
	}{
		{
			name:     "Valid page and pageSize",
			page:     2,
			pageSize: 10,
			expected: Options{Offset: 10, Limit: 10},
		},
		{
			name:     "Page is zero",
			page:     0,
			pageSize: 10,
			expected: Options{Offset: 0, Limit: 10},
		},
		{
			name:     "Page is negative",
			page:     -2,
			pageSize: 10,
			expected: Options{Offset: 0, Limit: 10},
		},
		{
			name:     "PageSize is zero falls back to the default",
			page:     2,
			pageSize: 0,
			expected: Options{Offset: page.DefaultSize, Limit: page.DefaultSize},
		},
		{
			name:     "PageSize is negative falls back to the default",
			page:     2,
			pageSize: -5,
			expected: Options{Offset: page.DefaultSize, Limit: page.DefaultSize},
		},
		{
			name:     "PageSize above the cap is clamped",
			page:     2,
			pageSize: page.MaxSize + 1,
			expected: Options{Offset: page.MaxSize, Limit: page.MaxSize},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			option := WithPage(tt.page, tt.pageSize)
			options := &Options{}
			option(options)

			if options.Offset != tt.expected.Offset || options.Limit != tt.expected.Limit {
				t.Errorf("Expected Offset: %d, Limit: %d, got Offset: %d, Limit: %d",
					tt.expected.Offset, tt.expected.Limit, options.Offset, options.Limit)
			}
		})
	}
}

func TestOptions_WithFilter(t *testing.T) {
	tests := []struct {
		name     string
		filter   map[any]any
		expected map[any]any
	}{
		{
			name:     "Single filter",
			filter:   map[any]any{"key1": "value1"},
			expected: map[any]any{"key1": "value1"},
		},
		{
			name:     "Multiple filters",
			filter:   map[any]any{"key1": "value1", "key2": "value2"},
			expected: map[any]any{"key1": "value1", "key2": "value2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			option := WithFilter(tt.filter)
			options := &Options{}
			option(options)

			if !reflect.DeepEqual(options.Filters, tt.expected) {
				t.Errorf("Expected Filters: %v, got: %v", tt.expected, options.Filters)
			}
		})
	}
}

type testRow struct {
	ID   uint
	Name string
}

func (testRow) TableName() string { return "test_rows" }

func newDryRunDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{DryRun: true})
	if err != nil {
		t.Fatalf("open dry-run db: %v", err)
	}
	return db
}

// Where 不能修改接收者：store.List 用同一个 Options 构造计数与分页两次查询，
// 旧实现每次调用都会把 Queries 转出的条件追加进 Clauses。
func TestOptionsWhereIsPure(t *testing.T) {
	db := newDryRunDB(t)
	opts := NewWhere(WithQuery("name = ?", "alice")).P(2, 10)

	opts.Where(db)
	afterFirst := len(opts.Clauses)
	opts.Where(db)

	if len(opts.Clauses) != afterFirst {
		t.Fatalf("Where modified Clauses: %d -> %d", afterFirst, len(opts.Clauses))
	}

	sql := db.ToSQL(func(tx *gorm.DB) *gorm.DB {
		return opts.Where(tx).Find(&[]testRow{})
	})
	if strings.Count(sql, "name = ") != 1 {
		t.Fatalf("duplicated condition in %q", sql)
	}
	if !strings.Contains(sql, "LIMIT 10 OFFSET 10") {
		t.Fatalf("pagination missing from %q", sql)
	}
}

func TestOptionsWhereAppliesOrders(t *testing.T) {
	db := newDryRunDB(t)
	columns, err := Sortable{"name": "name", "id": "id"}.Parse("-name,id")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	opts := NewWhere(WithPage(1, 5)).S(columns...)
	sql := db.ToSQL(func(tx *gorm.DB) *gorm.DB {
		return opts.Where(tx).Find(&[]testRow{})
	})

	if !strings.Contains(sql, "ORDER BY `name` DESC,`id`") {
		t.Fatalf("orders missing from %q", sql)
	}
	if !strings.Contains(sql, "LIMIT 5") {
		t.Fatalf("limit missing from %q", sql)
	}
}

// (*Options)(nil) 通过 where.Where 接口传进来时，普通的 whr != nil 判断拦不住，
// 旧实现会在这里空指针 panic。
func TestOptionsWhereNilReceiver(t *testing.T) {
	db := newDryRunDB(t)

	var opts *Options
	sql := db.ToSQL(func(tx *gorm.DB) *gorm.DB {
		return opts.Where(tx).Find(&[]testRow{})
	})

	if strings.Contains(sql, "WHERE") {
		t.Fatalf("nil Options must not add conditions: %q", sql)
	}
}

func sqlOf(t *testing.T, w Where) string {
	t.Helper()
	db := newDryRunDB(t)
	return db.ToSQL(func(tx *gorm.DB) *gorm.DB { return w.Where(tx).Find(&[]testRow{}) })
}

// 链式 C / WithClauses 追加的条件最终出现在 SQL 里，且包级入口 O/L/P/C/F 与方法等价。
func TestOptions_ClausesAndPackageHelpers(t *testing.T) {
	cond := clause.Gt{Column: "age", Value: 18}

	sql := sqlOf(t, C(cond))
	if !strings.Contains(sql, "`age` > 18") {
		t.Fatalf("C: %q", sql)
	}
	sql = sqlOf(t, NewWhere(WithClauses(cond, clause.Lt{Column: "age", Value: 60})))
	if !strings.Contains(sql, "`age` > 18") || !strings.Contains(sql, "`age` < 60") {
		t.Fatalf("WithClauses: %q", sql)
	}

	if o := O(7); o.Offset != 7 {
		t.Fatalf("O(7).Offset = %d", o.Offset)
	}
	if o := L(7); o.Limit != 7 {
		t.Fatalf("L(7).Limit = %d", o.Limit)
	}
	if o := P(3, 10); o.Offset != 20 || o.Limit != 10 {
		t.Fatalf("P(3,10) = %d/%d", o.Offset, o.Limit)
	}
	if o := F("name", "bob"); o.Filters["name"] != "bob" {
		t.Fatalf("F: %v", o.Filters)
	}
}

func TestWithOptions(t *testing.T) {
	o := NewWhere(WithOptions(func(o *Options) { o.Offset, o.Limit = 4, 9 }))
	if o.Offset != 4 || o.Limit != 9 {
		t.Fatalf("WithOptions = %d/%d, want 4/9", o.Offset, o.Limit)
	}
}

func TestOptions_C_Chain(t *testing.T) {
	o := NewWhere().C(clause.Gt{Column: "a", Value: 1}).C(clause.Lt{Column: "a", Value: 9})
	if len(o.Clauses) != 2 {
		t.Fatalf("len(Clauses) = %d, want 2", len(o.Clauses))
	}
}

// RegisterTenant 之后包级 T(ctx) 带上租户过滤；方法 T 同理。
func TestTenant_PackageT(t *testing.T) {
	type k struct{}
	RegisterTenant("tenant_id", func(ctx context.Context) string { return ctx.Value(k{}).(string) })
	t.Cleanup(func() { RegisterTenant("", nil) })

	ctx := context.WithValue(context.Background(), k{}, "t-42")
	if v := T(ctx).Filters["tenant_id"]; v != "t-42" {
		t.Fatalf("T(ctx).Filters = %v", T(ctx).Filters)
	}
}

// 奇数个参数：不 panic、不添加任何过滤条件，错误在执行时返回，查询不会执行。
func TestOptions_F_OddArgsReturnsError(t *testing.T) {
	for _, kvs := range [][]any{{"k"}, {"a", 1, "b"}} {
		var o *Options
		if !didNotPanic(func() { o = NewWhere().F(kvs...) }) {
			t.Fatalf("F(%v) must not panic", kvs)
		}
		if len(o.Filters) != 0 {
			t.Fatalf("F(%v) added filters despite the error: %v", kvs, o.Filters)
		}

		db := newDryRunDB(t)
		got := o.Where(db)
		if !errors.Is(got.Error, ErrInvalidFilter) {
			t.Fatalf("Where().Error = %v, want ErrInvalidFilter", got.Error)
		}
		if db.Error != nil {
			t.Fatalf("Where polluted the shared db: %v", db.Error)
		}
		if err := got.Find(&[]testRow{}).Error; !errors.Is(err, ErrInvalidFilter) {
			t.Fatalf("Find err = %v, want ErrInvalidFilter", err)
		}
	}
	if o := NewWhere().F(); len(o.Filters) != 0 || o.err != nil {
		t.Fatalf("F() with no args should be a no-op, got %v / %v", o.Filters, o.err)
	}
}

// 错误是粘性的：之后再链式调用 F/Q/P，错误不会消失；只记录第一个错误。
func TestOptions_F_ErrorIsSticky(t *testing.T) {
	o := NewWhere().F("a").F("b", 1).P(1, 5).Q("x = ?", 1)
	if !errors.Is(o.err, ErrInvalidFilter) || !strings.Contains(o.err.Error(), "got 1 arguments") {
		t.Fatalf("err = %v, want the first ErrInvalidFilter", o.err)
	}
	if err := o.Where(newDryRunDB(t)).Find(&[]testRow{}).Error; !errors.Is(err, ErrInvalidFilter) {
		t.Fatalf("Find err = %v", err)
	}
}

func didNotPanic(fn func()) (ok bool) {
	defer func() { ok = recover() == nil }()
	fn()
	return
}

// 没有注册租户时，包级 T 与方法 T 一致：不报错也不加过滤条件。
func TestTenant_UnregisteredIsNoOp(t *testing.T) {
	RegisterTenant("", nil)
	t.Cleanup(func() { RegisterTenant("", nil) })

	if o := T(context.Background()); len(o.Filters) != 0 {
		t.Fatalf("T() without a registered tenant added filters: %v", o.Filters)
	}
	if o := NewWhere().T(context.Background()); len(o.Filters) != 0 {
		t.Fatalf("(*Options).T() without a registered tenant added filters: %v", o.Filters)
	}
}

// 并发注册与读取租户不能有数据竞争（配合 -race 运行）。
func TestTenant_ConcurrentRegisterAndRead(t *testing.T) {
	t.Cleanup(func() { RegisterTenant("", nil) })
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			RegisterTenant("tenant_id", func(context.Context) string { return fmt.Sprint(i) })
		}()
		go func() {
			defer wg.Done()
			_ = T(context.Background())
		}()
	}
	wg.Wait()
}
