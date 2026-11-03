package repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// MongoQuery implements the Query interface for MongoDB
// | MongoQuery 是 Query 接口的 MongoDB 实现。
type MongoQuery[T any] struct {
	collection *mongo.Collection
	filter     bson.M
	sort       bson.D
	limit      int64
	skip       int64
	projection bson.M
	model      T
}

// Where adds an equality condition | Where 添加等值条件。
func (q *MongoQuery[T]) Where(field string, value any) Query[T] {
	q.filter[field] = value
	return q
}

// WhereIn adds an $in condition | WhereIn 添加 $in 条件。
func (q *MongoQuery[T]) WhereIn(field string, values []any) Query[T] {
	if q.filter == nil {
		q.filter = bson.M{}
	}
	q.filter[field] = bson.M{"$in": values}
	return q
}

// WhereNotIn adds an $nin condition | WhereNotIn 添加 $nin 条件。
func (q *MongoQuery[T]) WhereNotIn(field string, values []any) Query[T] {
	if q.filter == nil {
		q.filter = bson.M{}
	}
	q.filter[field] = bson.M{"$nin": values}
	return q
}

// WhereBetween adds a WHERE BETWEEN condition | WhereBetween 添加区间条件。
func (q *MongoQuery[T]) WhereBetween(field string, start, end any) Query[T] {
	if q.filter == nil {
		q.filter = bson.M{}
	}
	q.filter[field] = bson.M{
		"$gte": start,
		"$lte": end,
	}
	return q
}

// WhereNull adds a null condition | WhereNull 添加为空条件。
func (q *MongoQuery[T]) WhereNull(field string) Query[T] {
	if q.filter == nil {
		q.filter = bson.M{}
	}
	q.filter[field] = bson.M{"$eq": nil}
	return q
}

// WhereNotNull adds a not-null condition | WhereNotNull 添加非空条件。
func (q *MongoQuery[T]) WhereNotNull(field string) Query[T] {
	if q.filter == nil {
		q.filter = bson.M{}
	}
	q.filter[field] = bson.M{"$ne": nil}
	return q
}

// OrWhere adds an OR condition | OrWhere 添加 OR 条件。
func (q *MongoQuery[T]) OrWhere(field string, value any) Query[T] {
	cond := bson.M{field: value}
	if len(q.filter) == 0 {
		q.filter = bson.M{"$or": []bson.M{cond}}
		return q
	}
	// Matches SQL's `a OR b`: keep the existing filter whole as the first
	// branch and the new condition as the second.
	// Putting $and and $or side by side at the top level would read as
	// "original AND new".
	// 与 SQL 的 `a OR b` 一致：把已有条件整体作为第一个分支，新条件作为第二个分支。
	// 顶层同时放 $and 与 $or 会变成「原条件 且 新条件」。
	if or, ok := q.filter["$or"].([]bson.M); ok && len(q.filter) == 1 {
		q.filter["$or"] = append(or, cond)
		return q
	}
	q.filter = bson.M{"$or": []bson.M{q.filter, cond}}
	return q
}

// With is not directly supported in MongoDB | With 在 MongoDB 上没有对应实现（no-op）。
func (q *MongoQuery[T]) With(relation string) Query[T] {
	// MongoDB doesn't support joins like SQL databases; this would need an
	// aggregation pipeline
	// MongoDB 不支持 SQL 式 join，需要聚合管道实现
	return q
}

// WithCount is not directly supported in MongoDB | WithCount 在 MongoDB 上没有对应实现（no-op）。
func (q *MongoQuery[T]) WithCount(relation string) Query[T] {
	// Would need aggregation pipeline implementation
	// 需要用聚合管道实现
	return q
}

// OrderBy adds ordering | OrderBy 添加排序。
func (q *MongoQuery[T]) OrderBy(field string, direction string) Query[T] {
	order := 1
	if direction == "DESC" || direction == "desc" {
		order = -1
	}
	q.sort = append(q.sort, bson.E{Key: field, Value: order})
	return q
}

// OrderByDesc adds descending order | OrderByDesc 添加降序。
func (q *MongoQuery[T]) OrderByDesc(field string) Query[T] {
	q.sort = append(q.sort, bson.E{Key: field, Value: -1})
	return q
}

// OrderByAsc adds ascending order | OrderByAsc 添加升序。
func (q *MongoQuery[T]) OrderByAsc(field string) Query[T] {
	q.sort = append(q.sort, bson.E{Key: field, Value: 1})
	return q
}

// Limit adds a limit | Limit 添加条数上限。
func (q *MongoQuery[T]) Limit(limit int) Query[T] {
	q.limit = int64(limit)
	return q
}

// Offset adds an offset | Offset 添加跳过条数。
func (q *MongoQuery[T]) Offset(offset int) Query[T] {
	q.skip = int64(offset)
	return q
}

// GroupBy is not directly supported in MongoDB find operations
// | GroupBy 在 MongoDB 的 find 操作中没有对应实现（no-op）。
func (q *MongoQuery[T]) GroupBy(fields ...string) Query[T] {
	// Would need aggregation pipeline implementation
	// 需要用聚合管道实现
	return q
}

// Having is not directly supported in MongoDB find operations
// | Having 在 MongoDB 的 find 操作中没有对应实现（no-op）。
func (q *MongoQuery[T]) Having(condition string, value any) Query[T] {
	// Would need aggregation pipeline implementation
	// 需要用聚合管道实现
	return q
}

// Select specifies fields to return | Select 指定返回字段。
func (q *MongoQuery[T]) Select(fields ...string) Query[T] {
	if q.projection == nil {
		q.projection = bson.M{}
	}
	for _, field := range fields {
		q.projection[field] = 1
	}
	return q
}

// Distinct is not implemented for MongoDB queries
// | Distinct 在查询上未实现（no-op）。
func (q *MongoQuery[T]) Distinct() Query[T] {
	// MongoDB handles distinct differently - would need special implementation
	// MongoDB 的 distinct 机制不同，需要专门实现
	return q
}

// Find executes the query and returns results | Find 执行查询并返回结果。
func (q *MongoQuery[T]) Find(ctx context.Context) ([]T, error) {
	opts := options.Find()

	if len(q.sort) > 0 {
		opts.SetSort(q.sort)
	}
	if q.limit > 0 {
		opts.SetLimit(q.limit)
	}
	if q.skip > 0 {
		opts.SetSkip(q.skip)
	}
	if q.projection != nil {
		opts.SetProjection(q.projection)
	}

	cursor, err := q.collection.Find(ctx, q.filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []T
	if err := cursor.All(ctx, &results); err != nil {
		return nil, err
	}

	return results, nil
}

// First gets the first result | First 取第一条结果。
func (q *MongoQuery[T]) First(ctx context.Context) (*T, error) {
	opts := options.FindOne()

	if len(q.sort) > 0 {
		opts.SetSort(q.sort)
	}
	if q.skip > 0 {
		opts.SetSkip(q.skip)
	}
	if q.projection != nil {
		opts.SetProjection(q.projection)
	}

	var result T
	err := q.collection.FindOne(ctx, q.filter, opts).Decode(&result)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrRecordNotFound
		}
		return nil, err
	}

	return &result, nil
}

// FirstOrFail gets the first result or returns an error
// | FirstOrFail 取第一条结果，取不到即返回错误。
func (q *MongoQuery[T]) FirstOrFail(ctx context.Context) (*T, error) {
	result, err := q.First(ctx)
	if err != nil {
		if errors.Is(err, ErrRecordNotFound) {
			return nil, fmt.Errorf("no records found matching query")
		}
		return nil, err
	}
	return result, nil
}

// Exists checks whether matching records exist | Exists 判断是否存在匹配记录。
func (q *MongoQuery[T]) Exists(ctx context.Context) (bool, error) {
	count, err := q.collection.CountDocuments(ctx, q.filter, options.Count().SetLimit(1))
	return count > 0, err
}

// DoesNotExist checks whether no records exist | DoesNotExist 判断是否无匹配记录。
func (q *MongoQuery[T]) DoesNotExist(ctx context.Context) (bool, error) {
	exists, err := q.Exists(ctx)
	return !exists, err
}

// Count counts matching records | Count 统计匹配记录数。
func (q *MongoQuery[T]) Count(ctx context.Context) (int64, error) {
	return q.collection.CountDocuments(ctx, q.filter)
}

// Pluck extracts values from one column | Pluck 提取单个字段的值。
func (q *MongoQuery[T]) Pluck(ctx context.Context, field string) ([]any, error) {
	// Set projection to only include the requested field
	// 投影只保留请求的字段
	opts := options.Find().SetProjection(bson.M{field: 1, "_id": 0})

	if len(q.sort) > 0 {
		opts.SetSort(q.sort)
	}
	if q.limit > 0 {
		opts.SetLimit(q.limit)
	}
	if q.skip > 0 {
		opts.SetSkip(q.skip)
	}

	cursor, err := q.collection.Find(ctx, q.filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []any
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			return nil, err
		}
		if val, ok := doc[field]; ok {
			results = append(results, val)
		}
	}

	return results, cursor.Err()
}

// Delete deletes matching records | Delete 删除匹配的记录。
func (q *MongoQuery[T]) Delete(ctx context.Context) error {
	_, err := q.collection.DeleteMany(ctx, q.filter)
	return err
}

// Update updates matching records, stamping updated_at
// | Update 更新匹配的记录，并写入 updated_at 时间戳。
func (q *MongoQuery[T]) Update(ctx context.Context, data map[string]any) error {
	// Set updated_at timestamp
	// 写入 updated_at 时间戳
	data["updated_at"] = primitive.NewDateTimeFromTime(time.Now())

	_, err := q.collection.UpdateMany(
		ctx,
		q.filter,
		bson.M{"$set": data},
	)
	return err
}

// Paginate creates a paginated result | Paginate 创建分页结果。
func (q *MongoQuery[T]) Paginate(page, perPage int) PaginatedResult[T] {
	return &MongoPaginatedResult[T]{
		query:   q,
		page:    page,
		perPage: perPage,
	}
}

// MongoPaginatedResult implements PaginatedResult for MongoDB
// | MongoPaginatedResult 是 PaginatedResult 的 MongoDB 实现。
type MongoPaginatedResult[T any] struct {
	query   *MongoQuery[T]
	page    int
	perPage int
}

// Execute executes the paginated query | Execute 执行分页查询。
func (p *MongoPaginatedResult[T]) Execute(ctx context.Context) (*PaginationMeta, []T, error) {
	// Count total records
	// 统计总记录数
	total, err := p.query.collection.CountDocuments(ctx, p.query.filter)
	if err != nil {
		return nil, nil, err
	}

	// Calculate pagination
	// 计算分页参数
	if p.page < 1 {
		p.page = 1
	}
	if p.perPage < 1 {
		p.perPage = 10
	}

	skip := int64((p.page - 1) * p.perPage)
	totalPages := int(total) / p.perPage
	if int(total)%p.perPage > 0 {
		totalPages++
	}

	// Set pagination options
	// 设置分页参数
	p.query.skip = skip
	p.query.limit = int64(p.perPage)

	// Get paginated results
	// 获取分页结果
	results, err := p.query.Find(ctx)
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
