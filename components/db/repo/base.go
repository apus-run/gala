// Package repo provides a generic GORM repository: CRUD, chained queries,
// bulk and atomic field operations behind one [Repository] interface |
// repo 提供泛型 GORM 仓储：以 [Repository] 接口收拢 CRUD、链式查询、批量与原子字段操作.
//
// # Notes | 说明
//
// GormRepository[T] implements [Repository]; query methods return
// [Query] for chaining Where / OrderBy / Limit and terminal calls
// (FindAll, Count, Pluck...). Not-found maps to [ErrRecordNotFound];
// multi-statement atomicity goes through WithTransaction or a *gorm.DB tx.
// GormRepository[T] 实现 [Repository]；查询方法返回 [Query] 供链式调用
// Where / OrderBy / Limit 与终结调用（FindAll、Count、Pluck...）。查不到记录
// 映射为 [ErrRecordNotFound]；多条语句的原子性经 WithTransaction 或
// *gorm.DB 事务完成。
//
// # Example | 示例
//
//	r := repo.NewGormRepository(db, User{}, "users")
//	users, err := r.Where("age", 18).OrderBy("id", "desc").Limit(10).FindAll(ctx)
package repo

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GormRepository is a GORM-based implementation of Repository | GormRepository 是 Repository 的 GORM 实现。
type GormRepository[T any] struct {
	db        *gorm.DB
	model     T
	tableName string
	tx        *gorm.DB // For transaction support | 用于事务支持。
}

// NewGormRepository creates a new GORM repository | NewGormRepository 创建一个新的 GORM 仓储。
func NewGormRepository[T any](db *gorm.DB, model T, tableName string) Repository[T] {
	return &GormRepository[T]{
		db:        db,
		model:     model,
		tableName: tableName,
	}
}

// getDB returns the appropriate database connection | getDB 返回当前应使用的数据库连接。
func (r *GormRepository[T]) getDB() *gorm.DB {
	if r.tx != nil {
		return r.tx
	}
	return r.db
}

// FindByID finds a record by its primary key | FindByID 按主键查找一条记录。
func (r *GormRepository[T]) FindByID(ctx context.Context, id any) (*T, error) {
	var result T
	// Use a primary-key equality condition instead of First(&result, id):
	// when id is a string, gorm would treat it as raw SQL.
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

// FindOrFail finds a record or returns an error | FindOrFail 查找记录，找不到时返回错误。
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

// First gets the first result | First 取第一条结果。
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

// FirstOrFail gets the first result or returns an error | FirstOrFail 取第一条结果，取不到时返回错误。
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

// FindAll gets all records | FindAll 取全部记录。
func (r *GormRepository[T]) FindAll(ctx context.Context) ([]T, error) {
	var results []T
	err := r.getDB().WithContext(ctx).Find(&results).Error
	return results, err
}

// Create inserts a new record | Create 插入一条新记录。
func (r *GormRepository[T]) Create(ctx context.Context, data *T) error {
	return r.getDB().WithContext(ctx).Create(data).Error
}

// Update updates an existing record | Update 更新一条已存在的记录。
func (r *GormRepository[T]) Update(ctx context.Context, id any, data *T) error {
	db := r.getDB().WithContext(ctx)
	result := db.Model(&r.model).Where("id = ?", id).Updates(data)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		// MySQL by default reports "rows actually changed", so an update with
		// identical values also yields 0 (unless the DSN sets clientFoundRows=true);
		// on 0 rows, double-check whether the record really does not exist.
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

// Delete deletes a record by ID | Delete 按 ID 删除一条记录。
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

// query starts a chain from the current connection (transaction included);
// condition building and field validation live in GormQuery.
// query 以当前连接（含事务）为起点开始一条链；条件构造与字段校验都在 GormQuery 里。
func (r *GormRepository[T]) query() *GormQuery[T] {
	return &GormQuery[T]{db: r.getDB(), model: r.model}
}

// Where creates a new query with a WHERE condition | Where 创建一个带 WHERE 条件的新查询。
func (r *GormRepository[T]) Where(field string, value any) Query[T] {
	return r.query().Where(field, value)
}

// WhereIn creates a new query with a WHERE IN condition | WhereIn 创建一个带 WHERE IN 条件的新查询。
func (r *GormRepository[T]) WhereIn(field string, values []any) Query[T] {
	return r.query().WhereIn(field, values)
}

// WhereNotIn creates a new query with a WHERE NOT IN condition | WhereNotIn 创建一个带 WHERE NOT IN 条件的新查询。
func (r *GormRepository[T]) WhereNotIn(field string, values []any) Query[T] {
	return r.query().WhereNotIn(field, values)
}

// WhereBetween creates a new query with a WHERE BETWEEN condition | WhereBetween 创建一个带 WHERE BETWEEN 条件的新查询。
func (r *GormRepository[T]) WhereBetween(field string, start, end any) Query[T] {
	return r.query().WhereBetween(field, start, end)
}

// WhereNull creates a new query with a WHERE NULL condition | WhereNull 创建一个带 WHERE NULL 条件的新查询。
func (r *GormRepository[T]) WhereNull(field string) Query[T] {
	return r.query().WhereNull(field)
}

// WhereNotNull creates a new query with a WHERE NOT NULL condition | WhereNotNull 创建一个带 WHERE NOT NULL 条件的新查询。
func (r *GormRepository[T]) WhereNotNull(field string) Query[T] {
	return r.query().WhereNotNull(field)
}

// With eager loads related data | With 预加载关联数据。
func (r *GormRepository[T]) With(relation string) Query[T] {
	return r.query().With(relation)
}

// OrderBy adds ordering to the query, see GormQuery.OrderBy | OrderBy 为查询添加排序，见 GormQuery.OrderBy。
func (r *GormRepository[T]) OrderBy(field string, direction string) Query[T] {
	return r.query().OrderBy(field, direction)
}

// Limit adds a limit to the query | Limit 为查询添加 limit。
func (r *GormRepository[T]) Limit(limit int) Query[T] {
	return &GormQuery[T]{
		db:    r.getDB().Limit(limit),
		model: r.model,
	}
}

// Offset adds an offset to the query | Offset 为查询添加 offset。
func (r *GormRepository[T]) Offset(offset int) Query[T] {
	return &GormQuery[T]{
		db:    r.getDB().Offset(offset),
		model: r.model,
	}
}

// Exists checks if any records match. It fetches a single row instead of
// counting all matching rows.
// Exists 检查是否有匹配记录。只取一行，不数全部匹配行。
func (r *GormRepository[T]) Exists(ctx context.Context) (bool, error) {
	var one int
	res := r.getDB().WithContext(ctx).Model(&r.model).Select("1").Limit(1).Scan(&one)
	return res.RowsAffected > 0, res.Error
}

// DoesNotExist checks if no records match | DoesNotExist 检查是否没有匹配记录。
func (r *GormRepository[T]) DoesNotExist(ctx context.Context) (bool, error) {
	exists, err := r.Exists(ctx)
	return !exists, err
}

// Count counts the number of records | Count 统计记录条数。
func (r *GormRepository[T]) Count(ctx context.Context) (int64, error) {
	var count int64
	err := r.getDB().WithContext(ctx).Model(&r.model).Count(&count).Error
	return count, err
}

// Pluck extracts values from a single column | Pluck 取单列的值。
func (r *GormRepository[T]) Pluck(ctx context.Context, field string) ([]any, error) {
	var results []any
	// Select the column explicitly: passed as a plain string, a field with
	// spaces or symbols would be treated by gorm as raw SQL.
	// 显式 Select 列：只传字符串时，字段含空格或符号会被 gorm 当原始 SQL。
	sel := clause.Select{Columns: []clause.Column{{Name: field}}}
	err := r.getDB().WithContext(ctx).Model(&r.model).Clauses(sel).Pluck(field, &results).Error
	return results, err
}

// PluckString extracts string values from a single column | PluckString 取单列的字符串值。
func (r *GormRepository[T]) PluckString(ctx context.Context, field string) ([]string, error) {
	var results []string
	// Select the column explicitly: passed as a plain string, a field with
	// spaces or symbols would be treated by gorm as raw SQL.
	// 显式 Select 列：只传字符串时，字段含空格或符号会被 gorm 当原始 SQL。
	sel := clause.Select{Columns: []clause.Column{{Name: field}}}
	err := r.getDB().WithContext(ctx).Model(&r.model).Clauses(sel).Pluck(field, &results).Error
	return results, err
}

// PluckInt extracts int values from a single column | PluckInt 取单列的整数值。
func (r *GormRepository[T]) PluckInt(ctx context.Context, field string) ([]int, error) {
	var results []int
	// Select the column explicitly: passed as a plain string, a field with
	// spaces or symbols would be treated by gorm as raw SQL.
	// 显式 Select 列：只传字符串时，字段含空格或符号会被 gorm 当原始 SQL。
	sel := clause.Select{Columns: []clause.Column{{Name: field}}}
	err := r.getDB().WithContext(ctx).Model(&r.model).Clauses(sel).Pluck(field, &results).Error
	return results, err
}

// CreateBatch inserts multiple records | CreateBatch 插入多条记录。
func (r *GormRepository[T]) CreateBatch(ctx context.Context, data []T) error {
	return r.getDB().WithContext(ctx).CreateInBatches(data, 100).Error
}

// UpdateBatch updates multiple records | UpdateBatch 更新多条记录。
func (r *GormRepository[T]) UpdateBatch(ctx context.Context, ids []any, data []T) error {
	if len(ids) != len(data) {
		return fmt.Errorf("ids and data must have the same length")
	}

	// Transaction nests as a savepoint inside an existing transaction and
	// rolls back on panic too.
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

// DeleteBatch deletes multiple records | DeleteBatch 删除多条记录。
func (r *GormRepository[T]) DeleteBatch(ctx context.Context, ids []any) error {
	return r.getDB().WithContext(ctx).Delete(&r.model, ids).Error
}

// Increment increases a field value | Increment 增加某个字段的值。
func (r *GormRepository[T]) Increment(ctx context.Context, id any, field string, value int) error {
	return r.getDB().WithContext(ctx).Model(&r.model).Where("id = ?", id).
		UpdateColumn(field, gorm.Expr("? + ?", clause.Column{Name: field}, value)).Error
}

// Decrement decreases a field value | Decrement 减少某个字段的值。
func (r *GormRepository[T]) Decrement(ctx context.Context, id any, field string, value int) error {
	return r.getDB().WithContext(ctx).Model(&r.model).Where("id = ?", id).
		UpdateColumn(field, gorm.Expr("? - ?", clause.Column{Name: field}, value)).Error
}

// WithTransaction creates a new repository instance with a transaction | WithTransaction 创建一个绑定事务的新仓储实例。
func (r *GormRepository[T]) WithTransaction(tx *gorm.DB) Repository[T] {
	return &GormRepository[T]{
		db:        r.db,
		model:     r.model,
		tableName: r.tableName,
		tx:        tx,
	}
}
