package repo

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GormRepository is a GORM-based implementation of Repository
type GormRepository[T any] struct {
	db        *gorm.DB
	model     T
	tableName string
	tx        *gorm.DB // For transaction support
}

// NewGormRepository creates a new GORM repository
func NewGormRepository[T any](db *gorm.DB, model T, tableName string) Repository[T] {
	return &GormRepository[T]{
		db:        db,
		model:     model,
		tableName: tableName,
	}
}

// getDB returns the appropriate database connection
func (r *GormRepository[T]) getDB() *gorm.DB {
	if r.tx != nil {
		return r.tx
	}
	return r.db
}

// FindByID finds a record by its primary key
func (r *GormRepository[T]) FindByID(ctx context.Context, id any) (*T, error) {
	var result T
	// 用主键等值条件而不是 First(&result, id)：id 为 string 时 gorm 会把它当原始 SQL。
	err := r.getDB().WithContext(ctx).First(&result, clause.Eq{Column: clause.PrimaryColumn, Value: id}).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRecordNotFound
		}
		return nil, err
	}
	return &result, nil
}

// FindOrFail finds a record or returns an error
func (r *GormRepository[T]) FindOrFail(ctx context.Context, id any) (*T, error) {
	result, err := r.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, ErrRecordNotFound) {
			return nil, fmt.Errorf("%s with id %v not found", r.tableName, id)
		}
		return nil, err
	}
	return result, nil
}

// First gets the first result
func (r *GormRepository[T]) First(ctx context.Context) (*T, error) {
	var result T
	err := r.getDB().WithContext(ctx).First(&result).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRecordNotFound
		}
		return nil, err
	}
	return &result, nil
}

// FirstOrFail gets the first result or returns an error
func (r *GormRepository[T]) FirstOrFail(ctx context.Context) (*T, error) {
	result, err := r.First(ctx)
	if err != nil {
		if errors.Is(err, ErrRecordNotFound) {
			return nil, fmt.Errorf("no %s found", r.tableName)
		}
		return nil, err
	}
	return result, nil
}

// FindAll gets all records
func (r *GormRepository[T]) FindAll(ctx context.Context) ([]T, error) {
	var results []T
	err := r.getDB().WithContext(ctx).Find(&results).Error
	return results, err
}

// Create inserts a new record
func (r *GormRepository[T]) Create(ctx context.Context, data *T) error {
	return r.getDB().WithContext(ctx).Create(data).Error
}

// Update updates an existing record
func (r *GormRepository[T]) Update(ctx context.Context, id any, data *T) error {
	db := r.getDB().WithContext(ctx)
	result := db.Model(&r.model).Where("id = ?", id).Updates(data)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		// MySQL 默认返回「实际改变的行数」，更新成相同的值也是 0（除非 DSN 带
		// clientFoundRows=true），所以 0 行时要再确认记录是否真的不存在。
		var n int64
		if err := db.Model(&r.model).Where("id = ?", id).Count(&n).Error; err != nil {
			return err
		}
		if n == 0 {
			return ErrRecordNotFound
		}
	}
	return nil
}

// Delete deletes a record by ID
func (r *GormRepository[T]) Delete(ctx context.Context, id any) error {
	result := r.getDB().WithContext(ctx).Delete(&r.model, clause.Eq{Column: clause.PrimaryColumn, Value: id})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrRecordNotFound
	}
	return nil
}

// query 以当前连接（含事务）为起点开始一条链；条件构造与字段校验都在 GormQuery 里。
func (r *GormRepository[T]) query() *GormQuery[T] {
	return &GormQuery[T]{db: r.getDB(), model: r.model}
}

// Where creates a new query with a WHERE condition
func (r *GormRepository[T]) Where(field string, value any) Query[T] {
	return r.query().Where(field, value)
}

// WhereIn creates a new query with a WHERE IN condition
func (r *GormRepository[T]) WhereIn(field string, values []any) Query[T] {
	return r.query().WhereIn(field, values)
}

// WhereNotIn creates a new query with a WHERE NOT IN condition
func (r *GormRepository[T]) WhereNotIn(field string, values []any) Query[T] {
	return r.query().WhereNotIn(field, values)
}

// WhereBetween creates a new query with a WHERE BETWEEN condition
func (r *GormRepository[T]) WhereBetween(field string, start, end any) Query[T] {
	return r.query().WhereBetween(field, start, end)
}

// WhereNull creates a new query with a WHERE NULL condition
func (r *GormRepository[T]) WhereNull(field string) Query[T] {
	return r.query().WhereNull(field)
}

// WhereNotNull creates a new query with a WHERE NOT NULL condition
func (r *GormRepository[T]) WhereNotNull(field string) Query[T] {
	return r.query().WhereNotNull(field)
}

// With eager loads related data
func (r *GormRepository[T]) With(relation string) Query[T] {
	return r.query().With(relation)
}

// OrderBy adds ordering to the query, see GormQuery.OrderBy.
func (r *GormRepository[T]) OrderBy(field string, direction string) Query[T] {
	return r.query().OrderBy(field, direction)
}

// Limit adds a limit to the query
func (r *GormRepository[T]) Limit(limit int) Query[T] {
	return &GormQuery[T]{
		db:    r.getDB().Limit(limit),
		model: r.model,
	}
}

// Offset adds an offset to the query
func (r *GormRepository[T]) Offset(offset int) Query[T] {
	return &GormQuery[T]{
		db:    r.getDB().Offset(offset),
		model: r.model,
	}
}

// Exists checks if any records match。只取一行，不数全部匹配行。
func (r *GormRepository[T]) Exists(ctx context.Context) (bool, error) {
	var one int
	res := r.getDB().WithContext(ctx).Model(&r.model).Select("1").Limit(1).Scan(&one)
	return res.RowsAffected > 0, res.Error
}

// DoesNotExist checks if no records match
func (r *GormRepository[T]) DoesNotExist(ctx context.Context) (bool, error) {
	exists, err := r.Exists(ctx)
	return !exists, err
}

// Count counts the number of records
func (r *GormRepository[T]) Count(ctx context.Context) (int64, error) {
	var count int64
	err := r.getDB().WithContext(ctx).Model(&r.model).Count(&count).Error
	return count, err
}

// Pluck extracts values from a single column
func (r *GormRepository[T]) Pluck(ctx context.Context, field string) ([]any, error) {
	var results []any
	// 显式 Select 列：只传字符串时，字段含空格或符号会被 gorm 当原始 SQL。
	sel := clause.Select{Columns: []clause.Column{{Name: field}}}
	err := r.getDB().WithContext(ctx).Model(&r.model).Clauses(sel).Pluck(field, &results).Error
	return results, err
}

// PluckString extracts string values from a single column
func (r *GormRepository[T]) PluckString(ctx context.Context, field string) ([]string, error) {
	var results []string
	// 显式 Select 列：只传字符串时，字段含空格或符号会被 gorm 当原始 SQL。
	sel := clause.Select{Columns: []clause.Column{{Name: field}}}
	err := r.getDB().WithContext(ctx).Model(&r.model).Clauses(sel).Pluck(field, &results).Error
	return results, err
}

// PluckInt extracts int values from a single column
func (r *GormRepository[T]) PluckInt(ctx context.Context, field string) ([]int, error) {
	var results []int
	// 显式 Select 列：只传字符串时，字段含空格或符号会被 gorm 当原始 SQL。
	sel := clause.Select{Columns: []clause.Column{{Name: field}}}
	err := r.getDB().WithContext(ctx).Model(&r.model).Clauses(sel).Pluck(field, &results).Error
	return results, err
}

// CreateBatch inserts multiple records
func (r *GormRepository[T]) CreateBatch(ctx context.Context, data []T) error {
	return r.getDB().WithContext(ctx).CreateInBatches(data, 100).Error
}

// UpdateBatch updates multiple records
func (r *GormRepository[T]) UpdateBatch(ctx context.Context, ids []any, data []T) error {
	if len(ids) != len(data) {
		return fmt.Errorf("ids and data must have the same length")
	}

	// Transaction 在已有事务里嵌套为 savepoint，panic 时也会回滚。
	return r.getDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for i, id := range ids {
			if err := tx.Model(&r.model).Where("id = ?", id).Updates(&data[i]).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// DeleteBatch deletes multiple records
func (r *GormRepository[T]) DeleteBatch(ctx context.Context, ids []any) error {
	return r.getDB().WithContext(ctx).Delete(&r.model, ids).Error
}

// Increment increases a field value
func (r *GormRepository[T]) Increment(ctx context.Context, id any, field string, value int) error {
	return r.getDB().WithContext(ctx).Model(&r.model).Where("id = ?", id).
		UpdateColumn(field, gorm.Expr("? + ?", clause.Column{Name: field}, value)).Error
}

// Decrement decreases a field value
func (r *GormRepository[T]) Decrement(ctx context.Context, id any, field string, value int) error {
	return r.getDB().WithContext(ctx).Model(&r.model).Where("id = ?", id).
		UpdateColumn(field, gorm.Expr("? - ?", clause.Column{Name: field}, value)).Error
}

// WithTransaction creates a new repository instance with a transaction
func (r *GormRepository[T]) WithTransaction(tx *gorm.DB) Repository[T] {
	return &GormRepository[T]{
		db:        r.db,
		model:     r.model,
		tableName: r.tableName,
		tx:        tx,
	}
}
