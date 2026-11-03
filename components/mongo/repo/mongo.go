package repo

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// WhereOperator starts a query with a comparison operator (=, >=, LIKE, IN,
// ...) | WhereOperator 以比较运算符（=、>=、LIKE、IN 等）开始一个查询。
func (r *MongoRepository[T]) WhereOperator(field string, operator string, value any) Query[T] {
	filter := toBSONFilter(field, operator, value)
	return &MongoQuery[T]{
		collection: r.collection,
		filter:     filter,
		model:      r.model,
	}
}

// WhereRaw creates a query with a raw BSON filter
// | WhereRaw 以原生 BSON 过滤器创建查询。
func (r *MongoRepository[T]) WhereRaw(filter bson.M) Query[T] {
	return &MongoQuery[T]{
		collection: r.collection,
		filter:     filter,
		model:      r.model,
	}
}

// Advanced query methods for MongoQuery | MongoQuery 的高级查询方法

// WhereOperator adds a condition with a comparison operator
// | WhereOperator 添加带比较运算符的条件。
func (q *MongoQuery[T]) WhereOperator(field string, operator string, value any) Query[T] {
	filter := toBSONFilter(field, operator, value)

	// Merge with existing filter
	// 与已有过滤条件合并
	maps.Copy(q.filter, filter)

	return q
}

// WhereLike adds a LIKE condition (using regex)
// | WhereLike 添加 LIKE 条件（用正则实现）。
func (q *MongoQuery[T]) WhereLike(field string, pattern string) Query[T] {
	// Convert SQL LIKE pattern to regex
	// 把 SQL LIKE 模式转成正则
	regexPattern := strings.ReplaceAll(pattern, "%", ".*")
	regexPattern = strings.ReplaceAll(regexPattern, "_", ".")

	q.filter[field] = bson.M{"$regex": regexPattern, "$options": "i"}
	return q
}

// WhereDate adds a date comparison | WhereDate 添加日期比较条件。
func (q *MongoQuery[T]) WhereDate(field string, operator string, date time.Time) Query[T] {
	dateTime := primitive.NewDateTimeFromTime(date)

	switch operator {
	case "=":
		// For an exact date match, check within the day
		// 精确匹配某天时，需要覆盖当天整天
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

// WhereJSON adds a JSON/nested field query | WhereJSON 添加 JSON/嵌套字段查询。
func (q *MongoQuery[T]) WhereJSON(jsonPath string, value any) Query[T] {
	q.filter[jsonPath] = value
	return q
}

// WhereExists checks that a field exists | WhereExists 检查字段是否存在。
func (q *MongoQuery[T]) WhereExists(field string) Query[T] {
	q.filter[field] = bson.M{"$exists": true}
	return q
}

// WhereType checks the BSON type of a field | WhereType 检查字段的 BSON 类型。
func (q *MongoQuery[T]) WhereType(field string, bsonType string) Query[T] {
	q.filter[field] = bson.M{"$type": bsonType}
	return q
}

// WhereRegex adds a regex condition | WhereRegex 添加正则条件。
func (q *MongoQuery[T]) WhereRegex(field string, pattern string, options string) Query[T] {
	q.filter[field] = bson.M{"$regex": pattern, "$options": options}
	return q
}

// WhereElemMatch matches array elements | WhereElemMatch 匹配数组元素（$elemMatch）。
func (q *MongoQuery[T]) WhereElemMatch(field string, match bson.M) Query[T] {
	q.filter[field] = bson.M{"$elemMatch": match}
	return q
}

// WhereSize checks the array size | WhereSize 检查数组长度（$size）。
func (q *MongoQuery[T]) WhereSize(field string, size int) Query[T] {
	q.filter[field] = bson.M{"$size": size}
	return q
}

// WhereAll checks that an array contains all elements
// | WhereAll 检查数组包含全部给定元素（$all）。
func (q *MongoQuery[T]) WhereAll(field string, values []any) Query[T] {
	q.filter[field] = bson.M{"$all": values}
	return q
}

// OrQuery combines multiple queries with OR | OrQuery 用 OR 组合多个查询。
func (q *MongoQuery[T]) OrQuery(queries ...bson.M) Query[T] {
	if len(queries) == 0 {
		return q
	}

	// If there's an existing filter, include it in the OR
	// 已有过滤条件时，把它并入 OR 的第一个分支
	if len(q.filter) > 0 {
		queries = append([]bson.M{q.filter}, queries...)
		q.filter = bson.M{}
	}

	q.filter["$or"] = queries
	return q
}

// AndQuery combines multiple queries with AND | AndQuery 用 AND 组合多个查询。
func (q *MongoQuery[T]) AndQuery(queries ...bson.M) Query[T] {
	if len(queries) == 0 {
		return q
	}

	// If there's an existing filter, include it in the AND
	// 已有过滤条件时，把它并入 AND 的第一个分支
	if len(q.filter) > 0 {
		queries = append([]bson.M{q.filter}, queries...)
		q.filter = bson.M{}
	}

	q.filter["$and"] = queries
	return q
}

// NorQuery combines multiple queries with NOR | NorQuery 用 NOR 组合多个查询。
func (q *MongoQuery[T]) NorQuery(queries ...bson.M) Query[T] {
	if len(queries) == 0 {
		return q
	}

	q.filter["$nor"] = queries
	return q
}

// toBSONFilter converts an operator to a BSON filter
// | toBSONFilter 把运算符转成 BSON 过滤器。
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

// Aggregation support for MongoDB | MongoDB 聚合支持

// Aggregate performs an aggregation pipeline | Aggregate 执行聚合管道。
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

// GroupBy performs a group-by aggregation | GroupBy 执行分组聚合。
func (r *MongoRepository[T]) GroupBy(ctx context.Context, groupFields []string, aggregations bson.M) ([]bson.M, error) {
	// Build the group stage
	// 构建 group 阶段
	groupStage := bson.M{"_id": bson.M{}}
	for _, field := range groupFields {
		groupStage["_id"].(bson.M)[field] = "$" + field
	}

	// Add aggregations
	// 加入聚合表达式
	maps.Copy(groupStage, aggregations)

	pipeline := []bson.M{
		{"$group": groupStage},
	}

	return r.Aggregate(ctx, pipeline)
}

// Text search support | 文本搜索支持

// TextSearch performs a $text search | TextSearch 执行 $text 文本搜索。
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

// Geospatial queries | 地理空间查询

// Near performs a geospatial $near query | Near 执行地理空间 $near 查询。
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

// Within performs a geospatial $geoWithin query | Within 执行地理空间 $geoWithin 查询。
func (q *MongoQuery[T]) Within(field string, geometry bson.M) Query[T] {
	q.filter[field] = bson.M{
		"$geoWithin": bson.M{
			"$geometry": geometry,
		},
	}
	return q
}
