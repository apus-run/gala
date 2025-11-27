package repo

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// MongoQuery implements the Query interface for MongoDB
type MongoQuery[T any] struct {
	collection *mongo.Collection
	filter     bson.M
	sort       bson.D
	limit      int64
	skip       int64
	projection bson.M
	model      T
}

// Where adds a WHERE condition
func (q *MongoQuery[T]) Where(field string, value any) Query[T] {
	q.filter[field] = value
	return q
}

func (q *MongoQuery[T]) WhereIn(field string, values []any) Query[T] {
	if q.filter == nil {
		q.filter = bson.M{}
	}
	q.filter[field] = bson.M{"$in": values}
	return q
}

func (q *MongoQuery[T]) WhereNotIn(field string, values []any) Query[T] {
	if q.filter == nil {
		q.filter = bson.M{}
	}
	q.filter[field] = bson.M{"$nin": values}
	return q
}

// WhereBetween adds a WHERE BETWEEN condition
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

func (q *MongoQuery[T]) WhereNull(field string) Query[T] {
	if q.filter == nil {
		q.filter = bson.M{}
	}
	q.filter[field] = bson.M{"$eq": nil}
	return q
}

func (q *MongoQuery[T]) WhereNotNull(field string) Query[T] {
	if q.filter == nil {
		q.filter = bson.M{}
	}
	q.filter[field] = bson.M{"$ne": nil}
	return q
}

// OrWhere adds an OR WHERE condition
func (q *MongoQuery[T]) OrWhere(field string, value any) Query[T] {
	// Convert existing filter to $and if needed
	if _, hasOr := q.filter["$or"]; !hasOr {
		if len(q.filter) > 0 {
			// Move existing conditions to $and
			andConditions := []bson.M{q.filter}
			q.filter = bson.M{"$and": andConditions}
		}
	}

	// Add OR condition
	orConditions, _ := q.filter["$or"].([]bson.M)
	orConditions = append(orConditions, bson.M{field: value})
	q.filter["$or"] = orConditions

	return q
}

// With is not directly supported in MongoDB
func (q *MongoQuery[T]) With(relation string) Query[T] {
	// MongoDB doesn't support joins like SQL databases
	// This would need to be implemented with aggregation pipeline
	return q
}

// WithCount is not directly supported in MongoDB
func (q *MongoQuery[T]) WithCount(relation string) Query[T] {
	// Would need aggregation pipeline implementation
	return q
}

// OrderBy adds ordering
func (q *MongoQuery[T]) OrderBy(field string, direction string) Query[T] {
	order := 1
	if direction == "DESC" || direction == "desc" {
		order = -1
	}
	q.sort = append(q.sort, bson.E{Key: field, Value: order})
	return q
}

// OrderByDesc adds descending order
func (q *MongoQuery[T]) OrderByDesc(field string) Query[T] {
	q.sort = append(q.sort, bson.E{Key: field, Value: -1})
	return q
}

// OrderByAsc adds ascending order
func (q *MongoQuery[T]) OrderByAsc(field string) Query[T] {
	q.sort = append(q.sort, bson.E{Key: field, Value: 1})
	return q
}

// Limit adds a limit
func (q *MongoQuery[T]) Limit(limit int) Query[T] {
	q.limit = int64(limit)
	return q
}

// Offset adds an offset
func (q *MongoQuery[T]) Offset(offset int) Query[T] {
	q.skip = int64(offset)
	return q
}

// GroupBy is not directly supported in MongoDB find operations
func (q *MongoQuery[T]) GroupBy(fields ...string) Query[T] {
	// Would need aggregation pipeline implementation
	return q
}

// Having is not directly supported in MongoDB find operations
func (q *MongoQuery[T]) Having(condition string, value any) Query[T] {
	// Would need aggregation pipeline implementation
	return q
}

// Select specifies fields to return
func (q *MongoQuery[T]) Select(fields ...string) Query[T] {
	if q.projection == nil {
		q.projection = bson.M{}
	}
	for _, field := range fields {
		q.projection[field] = 1
	}
	return q
}

// Distinct adds DISTINCT functionality
func (q *MongoQuery[T]) Distinct() Query[T] {
	// MongoDB handles distinct differently - would need special implementation
	return q
}

// Find executes the query and returns results
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

// First gets the first result
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
		if err == mongo.ErrNoDocuments {
			return nil, ErrRecordNotFound
		}
		return nil, err
	}

	return &result, nil
}

// FirstOrFail gets the first result or returns an error
func (q *MongoQuery[T]) FirstOrFail(ctx context.Context) (*T, error) {
	result, err := q.First(ctx)
	if err != nil {
		if err == ErrRecordNotFound {
			return nil, fmt.Errorf("no records found matching query")
		}
		return nil, err
	}
	return result, nil
}

// Exists checks if records exist
func (q *MongoQuery[T]) Exists(ctx context.Context) (bool, error) {
	count, err := q.collection.CountDocuments(ctx, q.filter, options.Count().SetLimit(1))
	return count > 0, err
}

// DoesNotExist checks if no records exist
func (q *MongoQuery[T]) DoesNotExist(ctx context.Context) (bool, error) {
	exists, err := q.Exists(ctx)
	return !exists, err
}

// Count counts matching records
func (q *MongoQuery[T]) Count(ctx context.Context) (int64, error) {
	return q.collection.CountDocuments(ctx, q.filter)
}

// Pluck extracts values from a column
func (q *MongoQuery[T]) Pluck(ctx context.Context, field string) ([]any, error) {
	// Set projection to only include the requested field
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

// Delete deletes matching records
func (q *MongoQuery[T]) Delete(ctx context.Context) error {
	_, err := q.collection.DeleteMany(ctx, q.filter)
	return err
}

// Update updates matching records
func (q *MongoQuery[T]) Update(ctx context.Context, data map[string]any) error {
	// Set updated_at timestamp
	data["updated_at"] = primitive.NewDateTimeFromTime(time.Now())

	_, err := q.collection.UpdateMany(
		ctx,
		q.filter,
		bson.M{"$set": data},
	)
	return err
}

// Paginate creates a paginated result
func (q *MongoQuery[T]) Paginate(page, perPage int) PaginatedResult[T] {
	return &MongoPaginatedResult[T]{
		query:   q,
		page:    page,
		perPage: perPage,
	}
}

// MongoPaginatedResult implements PaginatedResult for MongoDB
type MongoPaginatedResult[T any] struct {
	query   *MongoQuery[T]
	page    int
	perPage int
}

// Execute executes the paginated query
func (p *MongoPaginatedResult[T]) Execute(ctx context.Context) (*PaginationMeta, []T, error) {
	// Count total records
	total, err := p.query.collection.CountDocuments(ctx, p.query.filter)
	if err != nil {
		return nil, nil, err
	}

	// Calculate pagination
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
	p.query.skip = skip
	p.query.limit = int64(p.perPage)

	// Get paginated results
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
