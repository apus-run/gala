package repo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func (r *MongoRepository[T]) WhereOperator(field string, operator string, value any) Query[T] {
	filter := toBSONFilter(field, operator, value)
	return &MongoQuery[T]{
		collection: r.collection,
		filter:     filter,
		model:      r.model,
	}
}

// WhereRaw creates a query with raw BSON filter
func (r *MongoRepository[T]) WhereRaw(filter bson.M) Query[T] {
	return &MongoQuery[T]{
		collection: r.collection,
		filter:     filter,
		model:      r.model,
	}
}

// Advanced query methods for MongoQuery

// WhereOperator adds a WHERE condition with operator
func (q *MongoQuery[T]) WhereOperator(field string, operator string, value any) Query[T] {
	filter := toBSONFilter(field, operator, value)

	// Merge with existing filter
	for k, v := range filter {
		q.filter[k] = v
	}

	return q
}

// WhereLike adds a LIKE condition (using regex)
func (q *MongoQuery[T]) WhereLike(field string, pattern string) Query[T] {
	// Convert SQL LIKE pattern to regex
	regexPattern := strings.ReplaceAll(pattern, "%", ".*")
	regexPattern = strings.ReplaceAll(regexPattern, "_", ".")

	q.filter[field] = bson.M{"$regex": regexPattern, "$options": "i"}
	return q
}

// WhereDate adds date comparison
func (q *MongoQuery[T]) WhereDate(field string, operator string, date time.Time) Query[T] {
	dateTime := primitive.NewDateTimeFromTime(date)

	switch operator {
	case "=":
		// For exact date match, we need to check within the day
		startOfDay := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
		endOfDay := startOfDay.Add(24 * time.Hour)
		q.filter[field] = bson.M{
			"$gte": primitive.NewDateTimeFromTime(startOfDay),
			"$lt":  primitive.NewDateTimeFromTime(endOfDay),
		}
	case ">":
		q.filter[field] = bson.M{"$gt": dateTime}
	case ">=":
		q.filter[field] = bson.M{"$gte": dateTime}
	case "<":
		q.filter[field] = bson.M{"$lt": dateTime}
	case "<=":
		q.filter[field] = bson.M{"$lte": dateTime}
	}

	return q
}

// WhereJSON adds JSON/nested field query
func (q *MongoQuery[T]) WhereJSON(jsonPath string, value any) Query[T] {
	q.filter[jsonPath] = value
	return q
}

// WhereExists checks if field exists
func (q *MongoQuery[T]) WhereExists(field string) Query[T] {
	q.filter[field] = bson.M{"$exists": true}
	return q
}

// WhereType checks field type
func (q *MongoQuery[T]) WhereType(field string, bsonType string) Query[T] {
	q.filter[field] = bson.M{"$type": bsonType}
	return q
}

// WhereRegex adds regex condition
func (q *MongoQuery[T]) WhereRegex(field string, pattern string, options string) Query[T] {
	q.filter[field] = bson.M{"$regex": pattern, "$options": options}
	return q
}

// WhereElemMatch for array queries
func (q *MongoQuery[T]) WhereElemMatch(field string, match bson.M) Query[T] {
	q.filter[field] = bson.M{"$elemMatch": match}
	return q
}

// WhereSize checks array size
func (q *MongoQuery[T]) WhereSize(field string, size int) Query[T] {
	q.filter[field] = bson.M{"$size": size}
	return q
}

// WhereAll checks if array contains all elements
func (q *MongoQuery[T]) WhereAll(field string, values []any) Query[T] {
	q.filter[field] = bson.M{"$all": values}
	return q
}

// OrQuery combines multiple queries with OR
func (q *MongoQuery[T]) OrQuery(queries ...bson.M) Query[T] {
	if len(queries) == 0 {
		return q
	}

	// If there's an existing filter, include it in the OR
	if len(q.filter) > 0 {
		queries = append([]bson.M{q.filter}, queries...)
		q.filter = bson.M{}
	}

	q.filter["$or"] = queries
	return q
}

// AndQuery combines multiple queries with AND
func (q *MongoQuery[T]) AndQuery(queries ...bson.M) Query[T] {
	if len(queries) == 0 {
		return q
	}

	// If there's an existing filter, include it in the AND
	if len(q.filter) > 0 {
		queries = append([]bson.M{q.filter}, queries...)
		q.filter = bson.M{}
	}

	q.filter["$and"] = queries
	return q
}

// NorQuery combines multiple queries with NOR
func (q *MongoQuery[T]) NorQuery(queries ...bson.M) Query[T] {
	if len(queries) == 0 {
		return q
	}

	q.filter["$nor"] = queries
	return q
}

// Helper function to convert operator to BSON filter
func toBSONFilter(field string, operator string, value any) bson.M {
	switch operator {
	case "=", "==":
		return bson.M{field: value}
	case ">":
		return bson.M{field: bson.M{"$gt": value}}
	case ">=":
		return bson.M{field: bson.M{"$gte": value}}
	case "<":
		return bson.M{field: bson.M{"$lt": value}}
	case "<=":
		return bson.M{field: bson.M{"$lte": value}}
	case "!=", "<>":
		return bson.M{field: bson.M{"$ne": value}}
	case "LIKE":
		pattern := strings.ReplaceAll(fmt.Sprintf("%v", value), "%", ".*")
		return bson.M{field: bson.M{"$regex": pattern, "$options": "i"}}
	case "NOT LIKE":
		pattern := strings.ReplaceAll(fmt.Sprintf("%v", value), "%", ".*")
		return bson.M{field: bson.M{"$not": bson.M{"$regex": pattern, "$options": "i"}}}
	case "IN":
		return bson.M{field: bson.M{"$in": value}}
	case "NOT IN":
		return bson.M{field: bson.M{"$nin": value}}
	default:
		return bson.M{field: value}
	}
}

// Aggregation support for MongoDB

// Aggregate performs an aggregation pipeline
func (r *MongoRepository[T]) Aggregate(ctx context.Context, pipeline []bson.M) ([]bson.M, error) {
	cursor, err := r.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []bson.M
	if err := cursor.All(ctx, &results); err != nil {
		return nil, err
	}

	return results, nil
}

// GroupBy performs a group by aggregation
func (r *MongoRepository[T]) GroupBy(ctx context.Context, groupFields []string, aggregations bson.M) ([]bson.M, error) {
	// Build group stage
	groupStage := bson.M{"_id": bson.M{}}
	for _, field := range groupFields {
		groupStage["_id"].(bson.M)[field] = "$" + field
	}

	// Add aggregations
	for k, v := range aggregations {
		groupStage[k] = v
	}

	pipeline := []bson.M{
		{"$group": groupStage},
	}

	return r.Aggregate(ctx, pipeline)
}

// Text search support

// TextSearch performs text search
func (r *MongoRepository[T]) TextSearch(ctx context.Context, searchText string) ([]T, error) {
	filter := bson.M{"$text": bson.M{"$search": searchText}}

	cursor, err := r.collection.Find(ctx, filter)
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

// Geospatial queries

// Near performs a geospatial near query
func (q *MongoQuery[T]) Near(field string, longitude, latitude, maxDistance float64) Query[T] {
	q.filter[field] = bson.M{
		"$near": bson.M{
			"$geometry": bson.M{
				"type":        "Point",
				"coordinates": []float64{longitude, latitude},
			},
			"$maxDistance": maxDistance,
		},
	}
	return q
}

// Within performs a geospatial within query
func (q *MongoQuery[T]) Within(field string, geometry bson.M) Query[T] {
	q.filter[field] = bson.M{
		"$geoWithin": bson.M{
			"$geometry": geometry,
		},
	}
	return q
}
