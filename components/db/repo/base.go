package repo

import (
	"context"
	"fmt"

	"gorm.io/gorm"
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
	err := r.getDB().WithContext(ctx).First(&result, id).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
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
		if err == ErrRecordNotFound {
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
		if err == gorm.ErrRecordNotFound {
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
		if err == ErrRecordNotFound {
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
	result := r.getDB().WithContext(ctx).Model(&r.model).Where("id = ?", id).Updates(data)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrRecordNotFound
	}
	return nil
}

// Delete deletes a record by ID
func (r *GormRepository[T]) Delete(ctx context.Context, id any) error {
	result := r.getDB().WithContext(ctx).Delete(&r.model, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrRecordNotFound
	}
	return nil
}

// Where creates a new query with a WHERE condition
func (r *GormRepository[T]) Where(field string, value any) Query[T] {
	return &GormQuery[T]{
		db:    r.getDB().Where(fmt.Sprintf("%s = ?", field), value),
		model: r.model,
	}
}

// WhereIn creates a new query with a WHERE IN condition
func (r *GormRepository[T]) WhereIn(field string, values []any) Query[T] {
	return &GormQuery[T]{
		db:    r.getDB().Where(fmt.Sprintf("%s IN ?", field), values),
		model: r.model,
	}
}

// WhereNotIn creates a new query with a WHERE NOT IN condition
func (r *GormRepository[T]) WhereNotIn(field string, values []any) Query[T] {
	return &GormQuery[T]{
		db:    r.getDB().Where(fmt.Sprintf("%s NOT IN ?", field), values),
		model: r.model,
	}
}

// WhereBetween creates a new query with a WHERE BETWEEN condition
func (r *GormRepository[T]) WhereBetween(field string, start, end any) Query[T] {
	return &GormQuery[T]{
		db:    r.getDB().Where(fmt.Sprintf("%s BETWEEN ? AND ?", field), start, end),
		model: r.model,
	}
}

// WhereNull creates a new query with a WHERE NULL condition
func (r *GormRepository[T]) WhereNull(field string) Query[T] {
	return &GormQuery[T]{
		db:    r.getDB().Where(fmt.Sprintf("%s IS NULL", field)),
		model: r.model,
	}
}

// WhereNotNull creates a new query with a WHERE NOT NULL condition
func (r *GormRepository[T]) WhereNotNull(field string) Query[T] {
	return &GormQuery[T]{
		db:    r.getDB().Where(fmt.Sprintf("%s IS NOT NULL", field)),
		model: r.model,
	}
}

// With eager loads related data
func (r *GormRepository[T]) With(relation string) Query[T] {
	return &GormQuery[T]{
		db:    r.getDB().Preload(relation),
		model: r.model,
	}
}

// OrderBy adds ordering to the query
func (r *GormRepository[T]) OrderBy(field string, direction string) Query[T] {
	return &GormQuery[T]{
		db:    r.getDB().Order(fmt.Sprintf("%s %s", field, direction)),
		model: r.model,
	}
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

// Exists checks if any records match
func (r *GormRepository[T]) Exists(ctx context.Context) (bool, error) {
	var count int64
	err := r.getDB().WithContext(ctx).Model(&r.model).Count(&count).Error
	return count > 0, err
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
	err := r.getDB().WithContext(ctx).Model(&r.model).Pluck(field, &results).Error
	return results, err
}

// PluckString extracts string values from a single column
func (r *GormRepository[T]) PluckString(ctx context.Context, field string) ([]string, error) {
	var results []string
	err := r.getDB().WithContext(ctx).Model(&r.model).Pluck(field, &results).Error
	return results, err
}

// PluckInt extracts int values from a single column
func (r *GormRepository[T]) PluckInt(ctx context.Context, field string) ([]int, error) {
	var results []int
	err := r.getDB().WithContext(ctx).Model(&r.model).Pluck(field, &results).Error
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

	tx := r.getDB().WithContext(ctx).Begin()
	for i, id := range ids {
		if err := tx.Model(&r.model).Where("id = ?", id).Updates(&data[i]).Error; err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit().Error
}

// DeleteBatch deletes multiple records
func (r *GormRepository[T]) DeleteBatch(ctx context.Context, ids []any) error {
	return r.getDB().WithContext(ctx).Delete(&r.model, ids).Error
}

// Increment increases a field value
func (r *GormRepository[T]) Increment(ctx context.Context, id any, field string, value int) error {
	return r.getDB().WithContext(ctx).Model(&r.model).Where("id = ?", id).
		UpdateColumn(field, gorm.Expr(fmt.Sprintf("%s + ?", field), value)).Error
}

// Decrement decreases a field value
func (r *GormRepository[T]) Decrement(ctx context.Context, id any, field string, value int) error {
	return r.getDB().WithContext(ctx).Model(&r.model).Where("id = ?", id).
		UpdateColumn(field, gorm.Expr(fmt.Sprintf("%s - ?", field), value)).Error
}

// WithTransaction creates a new repository instance with a transaction
func (r *GormRepository[T]) WithTransaction(tx any) Repository[T] {
	gormTx, ok := tx.(*gorm.DB)
	if !ok {
		panic("invalid transaction type for GORM repository")
	}

	return &GormRepository[T]{
		db:        r.db,
		model:     r.model,
		tableName: r.tableName,
		tx:        gormTx,
	}
}
