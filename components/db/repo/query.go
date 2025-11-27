package repo

import (
	"context"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GormQuery implements the Query interface for GORM
type GormQuery[T any] struct {
	db    *gorm.DB
	model T
}

// Where adds a WHERE condition using clause.Eq
func (q *GormQuery[T]) Where(field string, value any) Query[T] {
	q.db = q.db.Clauses(clause.Where{Exprs: []clause.Expression{
		clause.Eq{Column: field, Value: value},
	}})
	return q
}

// WhereIn adds a WHERE IN condition using clause.IN
func (q *GormQuery[T]) WhereIn(field string, values []any) Query[T] {
	q.db = q.db.Clauses(clause.Where{Exprs: []clause.Expression{
		clause.IN{Column: field, Values: values},
	}})
	return q
}

// WhereNotIn adds a WHERE NOT IN condition using clause.Not and clause.IN
func (q *GormQuery[T]) WhereNotIn(field string, values []any) Query[T] {
	q.db = q.db.Clauses(clause.Where{Exprs: []clause.Expression{
		clause.Not(clause.IN{Column: field, Values: values}),
	}})
	return q
}

// WhereBetween adds a WHERE BETWEEN condition using clause.And with Gte and Lte
func (q *GormQuery[T]) WhereBetween(field string, start, end any) Query[T] {
	q.db = q.db.Clauses(clause.Where{Exprs: []clause.Expression{
		clause.And(
			clause.Gte{Column: clause.Column{Name: field}, Value: start},
			clause.Lte{Column: clause.Column{Name: field}, Value: end},
		),
	}})
	return q
}

// WhereNull adds a WHERE IS NULL condition using clause.IsNull
func (q *GormQuery[T]) WhereNull(field string) Query[T] {
	q.db = q.db.Where(clause.Expr{SQL: field + " IS NULL"})
	return q
}

// WhereNotNull adds a WHERE IS NOT NULL condition using clause.Not and clause.IsNull
func (q *GormQuery[T]) WhereNotNull(field string) Query[T] {
	q.db = q.db.Where(clause.Expr{SQL: field + " IS NOT NULL"})
	return q
}

// OrWhere adds an OR condition using clause.Or and clause.Eq
func (q *GormQuery[T]) OrWhere(field string, value any) Query[T] {
	q.db = q.db.Clauses(clause.Where{Exprs: []clause.Expression{
		clause.Or(clause.Eq{Column: field, Value: value}),
	}})
	return q
}

// With preloads a relationship
func (q *GormQuery[T]) With(relation string) Query[T] {
	q.db = q.db.Preload(relation)
	return q
}

// WithCount preloads a relationship count
func (q *GormQuery[T]) WithCount(relation string) Query[T] {
	// GORM doesn't have a direct WithCount, so we'll use a custom approach
	// This would need to be implemented based on your specific needs
	q.db = q.db.Preload(relation)
	return q
}

// OrderBy adds ordering
func (q *GormQuery[T]) OrderBy(field string, direction string) Query[T] {
	q.db = q.db.Order(fmt.Sprintf("%s %s", field, direction))
	return q
}

// OrderByDesc adds descending order
func (q *GormQuery[T]) OrderByDesc(field string) Query[T] {
	q.db = q.db.Order(fmt.Sprintf("%s DESC", field))
	return q
}

// OrderByAsc adds ascending order
func (q *GormQuery[T]) OrderByAsc(field string) Query[T] {
	q.db = q.db.Order(fmt.Sprintf("%s ASC", field))
	return q
}

// Limit adds a limit
func (q *GormQuery[T]) Limit(limit int) Query[T] {
	q.db = q.db.Limit(limit)
	return q
}

// Offset adds an offset
func (q *GormQuery[T]) Offset(offset int) Query[T] {
	q.db = q.db.Offset(offset)
	return q
}

// GroupBy adds grouping
func (q *GormQuery[T]) GroupBy(fields ...string) Query[T] {
	q.db = q.db.Group(fmt.Sprintf("%s", fields[0]))
	for _, field := range fields[1:] {
		q.db = q.db.Group(field)
	}
	return q
}

// Having adds a HAVING clause
func (q *GormQuery[T]) Having(condition string, value any) Query[T] {
	q.db = q.db.Having(condition, value)
	return q
}

// Select specifies fields to select
func (q *GormQuery[T]) Select(fields ...string) Query[T] {
	q.db = q.db.Select(fields)
	return q
}

// Distinct adds DISTINCT
func (q *GormQuery[T]) Distinct() Query[T] {
	q.db = q.db.Distinct()
	return q
}

// Find executes the query and returns results
func (q *GormQuery[T]) Find(ctx context.Context) ([]T, error) {
	var results []T
	err := q.db.WithContext(ctx).Find(&results).Error
	if err != nil {
		return nil, err
	}
	return results, nil
}

// First gets the first result
func (q *GormQuery[T]) First(ctx context.Context) (*T, error) {
	var result T
	err := q.db.WithContext(ctx).First(&result).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrRecordNotFound
		}
		return nil, err
	}
	return &result, nil
}

// FirstOrFail gets the first result or returns an error
func (q *GormQuery[T]) FirstOrFail(ctx context.Context) (*T, error) {
	result, err := q.First(ctx)
	if err != nil {
		if err == ErrRecordNotFound {
			return nil, fmt.Errorf("no records found")
		}
		return nil, err
	}
	return result, nil
}

// Exists checks if records exist
func (q *GormQuery[T]) Exists(ctx context.Context) (bool, error) {
	var count int64
	err := q.db.WithContext(ctx).Model(&q.model).Count(&count).Error
	return count > 0, err
}

// DoesNotExist checks if no records exist
func (q *GormQuery[T]) DoesNotExist(ctx context.Context) (bool, error) {
	exists, err := q.Exists(ctx)
	return !exists, err
}

// Count counts matching records
func (q *GormQuery[T]) Count(ctx context.Context) (int64, error) {
	var count int64
	err := q.db.WithContext(ctx).Model(&q.model).Count(&count).Error
	return count, err
}

// Pluck extracts values from a column
func (q *GormQuery[T]) Pluck(ctx context.Context, field string) ([]any, error) {
	var results []any
	err := q.db.WithContext(ctx).Model(&q.model).Pluck(field, &results).Error
	return results, err
}

// Delete deletes matching records
func (q *GormQuery[T]) Delete(ctx context.Context) error {
	return q.db.WithContext(ctx).Delete(&q.model).Error
}

// Update updates matching records
func (q *GormQuery[T]) Update(ctx context.Context, data map[string]any) error {
	return q.db.WithContext(ctx).Model(&q.model).Updates(data).Error
}

// Paginate creates a paginated result
func (q *GormQuery[T]) Paginate(page, perPage int) PaginatedResult[T] {
	return &GormPaginatedResult[T]{
		query:   q,
		page:    page,
		perPage: perPage,
	}
}

// GormPaginatedResult implements PaginatedResult for GORM
type GormPaginatedResult[T any] struct {
	query   *GormQuery[T]
	page    int
	perPage int
}

// Execute executes the paginated query
func (p *GormPaginatedResult[T]) Execute(ctx context.Context) (*PaginationMeta, []T, error) {
	// Count total records
	var total int64
	if err := p.query.db.Model(&p.query.model).Count(&total).Error; err != nil {
		return nil, nil, err
	}

	// Calculate pagination
	if p.page < 1 {
		p.page = 1
	}
	if p.perPage < 1 {
		p.perPage = 10
	}

	offset := (p.page - 1) * p.perPage
	totalPages := int(total) / p.perPage
	if int(total)%p.perPage > 0 {
		totalPages++
	}

	// Get paginated results
	var results []T
	err := p.query.db.WithContext(ctx).
		Limit(p.perPage).
		Offset(offset).
		Find(&results).Error

	if err != nil {
		return nil, nil, err
	}

	meta := &PaginationMeta{
		Page:       p.page,
		PerPage:    p.perPage,
		Total:      total,
		TotalPages: totalPages,
		HasNext:    p.page < totalPages,
		HasPrev:    p.page > 1,
	}

	return meta, results, nil
}
