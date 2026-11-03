package repo

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/apus-run/gala/components/db/page"
)

// GormQuery implements the Query interface for GORM | GormQuery 是 Query 接口的 GORM 实现。
type GormQuery[T any] struct {
	db    *gorm.DB
	model T
}

// Where adds a WHERE condition using clause.Eq | Where 用 clause.Eq 添加 WHERE 条件。
func (q *GormQuery[T]) Where(field string, value any) Query[T] {
	q.db = q.db.Clauses(clause.Where{Exprs: []clause.Expression{
		clause.Eq{Column: field, Value: value},
	}})
	return q
}

// WhereIn adds a WHERE IN condition using clause.IN | WhereIn 用 clause.IN 添加 WHERE IN 条件。
func (q *GormQuery[T]) WhereIn(field string, values []any) Query[T] {
	q.db = q.db.Clauses(clause.Where{Exprs: []clause.Expression{
		clause.IN{Column: field, Values: values},
	}})
	return q
}

// WhereNotIn adds a WHERE NOT IN condition using clause.Not and clause.IN | WhereNotIn 用 clause.Not 和 clause.IN 添加 WHERE NOT IN 条件。
func (q *GormQuery[T]) WhereNotIn(field string, values []any) Query[T] {
	q.db = q.db.Clauses(clause.Where{Exprs: []clause.Expression{
		clause.Not(clause.IN{Column: field, Values: values}),
	}})
	return q
}

// WhereBetween adds a WHERE BETWEEN condition using clause.And with Gte and Lte | WhereBetween 用 clause.And 组合 Gte 和 Lte 添加 WHERE BETWEEN 条件。
func (q *GormQuery[T]) WhereBetween(field string, start, end any) Query[T] {
	q.db = q.db.Clauses(clause.Where{Exprs: []clause.Expression{
		clause.And(
			clause.Gte{Column: clause.Column{Name: field}, Value: start},
			clause.Lte{Column: clause.Column{Name: field}, Value: end},
		),
	}})
	return q
}

// WhereNull adds a WHERE IS NULL condition using clause.IsNull | WhereNull 用 clause.IsNull 添加 WHERE IS NULL 条件。
func (q *GormQuery[T]) WhereNull(field string) Query[T] {
	q.db = q.db.Clauses(clause.Where{Exprs: []clause.Expression{
		clause.Eq{Column: field, Value: nil}, // Emits IS NULL; gorm quotes the column name | 生成 IS NULL，列名由 gorm 加引号。
	}})
	return q
}

// WhereNotNull adds a WHERE IS NOT NULL condition using clause.Neq | WhereNotNull 用 clause.Neq 添加 WHERE IS NOT NULL 条件。
func (q *GormQuery[T]) WhereNotNull(field string) Query[T] {
	q.db = q.db.Clauses(clause.Where{Exprs: []clause.Expression{
		clause.Neq{Column: field, Value: nil}, // Emits IS NOT NULL | 生成 IS NOT NULL。
	}})
	return q
}

// OrWhere adds an OR condition using clause.Or and clause.Eq | OrWhere 用 clause.Or 和 clause.Eq 添加 OR 条件。
func (q *GormQuery[T]) OrWhere(field string, value any) Query[T] {
	q.db = q.db.Clauses(clause.Where{Exprs: []clause.Expression{
		clause.Or(clause.Eq{Column: field, Value: value}),
	}})
	return q
}

// With preloads a relationship | With 预加载关联。
func (q *GormQuery[T]) With(relation string) Query[T] {
	q.db = q.db.Preload(relation)
	return q
}

// OrderBy adds ordering. direction "DESC" (case-insensitive) sorts descending
// and everything else ascending; gorm quotes the column name. Sort fields
// coming from a request should still go through the where.Sortable whitelist.
// OrderBy 添加排序。direction 为 "DESC"（不区分大小写）时倒序，其余一律正序；
// 列名由 gorm 加引号。请求带来的排序字段仍应走 where.Sortable 白名单。
func (q *GormQuery[T]) OrderBy(field string, direction string) Query[T] {
	q.db = q.db.Order(clause.OrderByColumn{Column: clause.Column{Name: field}, Desc: strings.EqualFold(direction, "DESC")})
	return q
}

// OrderByDesc adds descending order, see OrderBy | OrderByDesc 添加倒序，见 OrderBy。
func (q *GormQuery[T]) OrderByDesc(field string) Query[T] {
	return q.OrderBy(field, "DESC")
}

// OrderByAsc adds ascending order, see OrderBy | OrderByAsc 添加正序，见 OrderBy。
func (q *GormQuery[T]) OrderByAsc(field string) Query[T] {
	return q.OrderBy(field, "ASC")
}

// Limit adds a limit | Limit 添加 limit。
func (q *GormQuery[T]) Limit(limit int) Query[T] {
	q.db = q.db.Limit(limit)
	return q
}

// Offset adds an offset | Offset 添加 offset。
func (q *GormQuery[T]) Offset(offset int) Query[T] {
	q.db = q.db.Offset(offset)
	return q
}

// GroupBy adds grouping. With no fields it is a no-op. db.Group(string) is
// not used: a field containing spaces or symbols would be treated by gorm
// as raw SQL.
// GroupBy 添加分组。不传字段时是空操作。
// 不用 db.Group(string)：字段含空格或符号时 gorm 会把它当原始 SQL。
func (q *GormQuery[T]) GroupBy(fields ...string) Query[T] {
	for _, field := range fields {
		q.db = q.db.Clauses(clause.GroupBy{Columns: []clause.Column{{Name: field}}})
	}
	return q
}

// Having adds a HAVING clause. condition is a raw SQL fragment and must be a
// compile-time constant; values go in through the value placeholder.
// Having 添加 HAVING 子句。condition 是原始 SQL 片段，只能是编译期常量，
// 值通过 value 占位符传入。
func (q *GormQuery[T]) Having(condition string, value any) Query[T] {
	q.db = q.db.Having(condition, value)
	return q
}

// Select specifies fields to select. fields are raw SQL fragments (COUNT(*) AS n
// is fine) and must be compile-time constants.
// Select 指定要查询的字段。fields 是原始 SQL 片段（可以写 COUNT(*) AS n），
// 只能是编译期常量。
func (q *GormQuery[T]) Select(fields ...string) Query[T] {
	q.db = q.db.Select(fields)
	return q
}

// Distinct adds DISTINCT | Distinct 添加 DISTINCT。
func (q *GormQuery[T]) Distinct() Query[T] {
	q.db = q.db.Distinct()
	return q
}

// Find executes the query and returns results | Find 执行查询并返回结果。
func (q *GormQuery[T]) Find(ctx context.Context) ([]T, error) {
	var results []T
	err := q.db.WithContext(ctx).Find(&results).Error
	if err != nil {
		return nil, err
	}
	return results, nil
}

// First gets the first result | First 取第一条结果。
func (q *GormQuery[T]) First(ctx context.Context) (*T, error) {
	var result T
	err := q.db.WithContext(ctx).First(&result).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRecordNotFound
		}
		return nil, err
	}
	return &result, nil
}

// FirstOrFail gets the first result or returns an error | FirstOrFail 取第一条结果，取不到时返回错误。
func (q *GormQuery[T]) FirstOrFail(ctx context.Context) (*T, error) {
	result, err := q.First(ctx)
	if err != nil {
		if errors.Is(err, ErrRecordNotFound) {
			return nil, fmt.Errorf("no records found")
		}
		return nil, err
	}
	return result, nil
}

// Exists checks if records exist. It fetches a single row instead of counting
// all matching rows.
// Exists 检查记录是否存在。只取一行，不数全部匹配行。
func (q *GormQuery[T]) Exists(ctx context.Context) (bool, error) {
	var one int
	res := q.db.WithContext(ctx).Model(&q.model).Select("1").Limit(1).Scan(&one)
	return res.RowsAffected > 0, res.Error
}

// DoesNotExist checks if no records exist | DoesNotExist 检查是否没有记录。
func (q *GormQuery[T]) DoesNotExist(ctx context.Context) (bool, error) {
	exists, err := q.Exists(ctx)
	return !exists, err
}

// Count counts matching records | Count 统计匹配的记录条数。
func (q *GormQuery[T]) Count(ctx context.Context) (int64, error) {
	var count int64
	err := q.db.WithContext(ctx).Model(&q.model).Count(&count).Error
	return count, err
}

// Pluck extracts values from a column | Pluck 取单列的值。
func (q *GormQuery[T]) Pluck(ctx context.Context, field string) ([]any, error) {
	var results []any
	// Select the column explicitly: passed as a plain string, a field with
	// spaces or symbols would be treated by gorm as raw SQL.
	// 显式 Select 列：只传字符串时，字段含空格或符号会被 gorm 当原始 SQL。
	sel := clause.Select{Distinct: q.db.Statement.Distinct, Columns: []clause.Column{{Name: field}}}
	err := q.db.WithContext(ctx).Model(&q.model).Clauses(sel).Pluck(field, &results).Error
	return results, err
}

// Delete deletes matching records | Delete 删除匹配的记录。
func (q *GormQuery[T]) Delete(ctx context.Context) error {
	return q.db.WithContext(ctx).Delete(&q.model).Error
}

// Update updates matching records | Update 更新匹配的记录。
func (q *GormQuery[T]) Update(ctx context.Context, data map[string]any) error {
	return q.db.WithContext(ctx).Model(&q.model).Updates(data).Error
}

// Paginate creates a paginated result | Paginate 创建一个分页结果。
func (q *GormQuery[T]) Paginate(page, perPage int) PaginatedResult[T] {
	return &GormPaginatedResult[T]{
		query:   q,
		page:    page,
		perPage: perPage,
	}
}

// GormPaginatedResult implements PaginatedResult for GORM | GormPaginatedResult 是 PaginatedResult 的 GORM 实现。
type GormPaginatedResult[T any] struct {
	query   *GormQuery[T]
	page    int
	perPage int
}

// Execute executes the paginated query | Execute 执行分页查询。
func (p *GormPaginatedResult[T]) Execute(ctx context.Context) (*PaginationMeta, []T, error) {
	params := page.Params{Page: p.page, Size: p.perPage}
	base := p.query.db.WithContext(ctx)

	// Undo any pagination the caller may have set, so total counts every match.
	// 取消调用方可能已经设置的分页，保证 total 是全量匹配数。
	var total int64
	if err := base.Model(&p.query.model).Offset(-1).Limit(-1).Count(&total).Error; err != nil {
		return nil, nil, err
	}

	meta := page.NewMeta(params, total)

	results := make([]T, 0)
	if err := base.Offset(params.Offset()).Limit(params.Limit()).Find(&results).Error; err != nil {
		return nil, nil, err
	}

	return &meta, results, nil
}
