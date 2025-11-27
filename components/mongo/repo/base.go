package repo

import (
	"context"
	"fmt"
	"reflect"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// MongoRepository is a MongoDB implementation of Repository
type MongoRepository[T any] struct {
	collection *mongo.Collection
	model      T
	session    mongo.SessionContext
}

// NewMongoRepository creates a new MongoDB repository
func NewMongoRepository[T any](collection *mongo.Collection, model T) Repository[T] {
	return &MongoRepository[T]{
		collection: collection,
		model:      model,
	}
}

func (r *MongoRepository[T]) CreateBatch(ctx context.Context, data []T) error {
	if len(data) == 0 {
		return nil
	}

	// Convert []T to []interface{}
	documents := make([]interface{}, len(data))
	for i, v := range data {
		documents[i] = v
	}

	_, err := r.collection.InsertMany(ctx, documents)
	return err
}

func (r *MongoRepository[T]) Create(ctx context.Context, data *T) error {
	_, err := r.collection.InsertOne(ctx, data)
	return err
}

// Update replaces the document with the given ID
func (r *MongoRepository[T]) Update(ctx context.Context, id any, data *T) error {
	objectID, err := r.toObjectID(id)
	if err != nil {
		return ErrInvalidID
	}
	_, err = r.collection.ReplaceOne(ctx, bson.M{"_id": objectID}, data)
	return err
}

// Delete removes the document with the given ID
func (r *MongoRepository[T]) Delete(ctx context.Context, id any) error {
	objectID, err := r.toObjectID(id)
	if err != nil {
		return ErrInvalidID
	}
	_, err = r.collection.DeleteOne(ctx, bson.M{"_id": objectID})
	return err
}

func (r *MongoRepository[T]) Where(field string, value any) Query[T] {
	filter := bson.M{field: value}
	return &MongoQuery[T]{
		collection: r.collection,
		filter:     filter,
		model:      r.model,
	}
}

// FindByID finds a record by its primary key
func (r *MongoRepository[T]) FindByID(ctx context.Context, id any) (*T, error) {
	var result T

	// Convert string ID to ObjectID if needed
	objectID, err := r.toObjectID(id)
	if err != nil {
		return nil, ErrInvalidID
	}

	err = r.collection.FindOne(ctx, bson.M{"_id": objectID}).Decode(&result)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, ErrRecordNotFound
		}
		return nil, err
	}

	return &result, nil
}

// FindOrFail finds a record or returns an error
func (r *MongoRepository[T]) FindOrFail(ctx context.Context, id any) (*T, error) {
	result, err := r.FindByID(ctx, id)
	if err != nil {
		if err == ErrRecordNotFound {
			return nil, fmt.Errorf("record with ID %v not found", id)
		}
		return nil, err
	}
	return result, nil
}

// First gets the first result from the collection
func (r *MongoRepository[T]) First(ctx context.Context) (*T, error) {
	var result T
	opts := options.FindOne().SetSort(bson.D{{Key: "_id", Value: 1}})
	err := r.collection.FindOne(ctx, bson.D{}, opts).Decode(&result)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, ErrRecordNotFound
		}
		return nil, err
	}
	return &result, nil
}

// FirstOrFail gets the first result or fails
func (r *MongoRepository[T]) FirstOrFail(ctx context.Context) (*T, error) {
	result, err := r.First(ctx)
	if err != nil {
		return nil, fmt.Errorf("no record found: %w", err)
	}
	return result, nil
}

// All returns all records from the collection
func (r *MongoRepository[T]) All(ctx context.Context) ([]T, error) {
	cursor, err := r.collection.Find(ctx, bson.D{})
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

// toObjectID tries to convert the given ID to a MongoDB ObjectID
func (r *MongoRepository[T]) toObjectID(id any) (primitive.ObjectID, error) {
	switch v := id.(type) {
	case string:
		return primitive.ObjectIDFromHex(v)
	case primitive.ObjectID:
		return v, nil
	default:
		return primitive.NilObjectID, fmt.Errorf("invalid ID type: %v", reflect.TypeOf(id))
	}
}

// Count returns the total number of documents in the collection
func (r *MongoRepository[T]) Count(ctx context.Context) (int64, error) {
	count, err := r.collection.CountDocuments(ctx, bson.D{})
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (r *MongoRepository[T]) Exists(ctx context.Context) (bool, error) {
	count, err := r.Count(ctx)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *MongoRepository[T]) DoesNotExist(ctx context.Context) (bool, error) {
	exists, err := r.Exists(ctx)
	return !exists, err
}

func (r *MongoRepository[T]) Pluck(ctx context.Context, field string) ([]any, error) {
	opts := options.Find().SetProjection(bson.M{field: 1, "_id": 0})
	cursor, err := r.collection.Find(ctx, bson.D{}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []bson.M
	if err := cursor.All(ctx, &results); err != nil {
		return nil, err
	}

	var plucked []any
	for _, doc := range results {
		if val, ok := doc[field]; ok {
			plucked = append(plucked, val)
		}
	}
	return plucked, nil
}

func (r *MongoRepository[T]) PluckString(ctx context.Context, field string) ([]string, error) {
	values, err := r.Pluck(ctx, field)
	if err != nil {
		return nil, err
	}
	var result []string
	for _, v := range values {
		if str, ok := v.(string); ok {
			result = append(result, str)
		}
	}
	return result, nil
}

func (r *MongoRepository[T]) PluckInt(ctx context.Context, field string) ([]int, error) {
	values, err := r.Pluck(ctx, field)
	if err != nil {
		return nil, err
	}
	var result []int
	for _, v := range values {
		if i, ok := v.(int); ok {
			result = append(result, i)
		}
	}
	return result, nil
}

// UpdateBatch updates multiple documents by their IDs
func (r *MongoRepository[T]) UpdateBatch(ctx context.Context, ids []any, data []T) error {
	if len(ids) != len(data) {
		return fmt.Errorf("ids and data length mismatch")
	}

	for i, id := range ids {
		objectID, err := r.toObjectID(id)
		if err != nil {
			return ErrInvalidID
		}
		_, err = r.collection.ReplaceOne(ctx, bson.M{"_id": objectID}, data[i])
		if err != nil {
			return err
		}
	}
	return nil
}

// DeleteBatch deletes multiple documents by their IDs
func (r *MongoRepository[T]) DeleteBatch(ctx context.Context, ids []any) error {
	var objectIDs []primitive.ObjectID
	for _, id := range ids {
		objectID, err := r.toObjectID(id)
		if err != nil {
			return ErrInvalidID
		}
		objectIDs = append(objectIDs, objectID)
	}

	_, err := r.collection.DeleteMany(ctx, bson.M{"_id": bson.M{"$in": objectIDs}})
	return err
}

// Increment performs an atomic increment on a numeric field
func (r *MongoRepository[T]) Increment(ctx context.Context, id any, field string, value int) error {
	objectID, err := r.toObjectID(id)
	if err != nil {
		return ErrInvalidID
	}
	_, err = r.collection.UpdateOne(ctx, bson.M{"_id": objectID}, bson.M{
		"$inc": bson.M{field: value},
	})
	return err
}

// Decrement performs an atomic decrement on a numeric field
func (r *MongoRepository[T]) Decrement(ctx context.Context, id any, field string, value int) error {
	return r.Increment(ctx, id, field, -value)
}

func (r *MongoRepository[T]) FindAll(ctx context.Context) ([]T, error) {
	return r.All(ctx)
}

func (r *MongoRepository[T]) Limit(limit int) Query[T] {
	return &MongoQuery[T]{
		collection: r.collection,
		filter:     bson.M{},
		model:      r.model,
		limit:      int64(limit),
	}
}

func (r *MongoRepository[T]) Offset(offset int) Query[T] {
	return &MongoQuery[T]{
		collection: r.collection,
		filter:     bson.M{},
		model:      r.model,
		skip:       int64(offset),
	}
}

func (r *MongoRepository[T]) OrderBy(field string, direction string) Query[T] {
	order := 1
	if direction == "desc" {
		order = -1
	}
	return &MongoQuery[T]{
		collection: r.collection,
		filter:     bson.M{},
		sort:       bson.D{{Key: field, Value: order}},
		model:      r.model,
	}
}

func (r *MongoRepository[T]) WhereBetween(field string, start, end any) Query[T] {
	filter := bson.M{
		field: bson.M{
			"$gte": start,
			"$lte": end,
		},
	}

	return &MongoQuery[T]{
		collection: r.collection,
		filter:     filter,
		model:      r.model,
	}
}

func (r *MongoRepository[T]) WhereIn(field string, values []any) Query[T] {
	filter := bson.M{
		field: bson.M{
			"$in": values,
		},
	}

	return &MongoQuery[T]{
		collection: r.collection,
		filter:     filter,
		model:      r.model,
	}
}

func (r *MongoRepository[T]) WhereNotIn(field string, values []any) Query[T] {
	filter := bson.M{
		field: bson.M{
			"$nin": values,
		},
	}

	return &MongoQuery[T]{
		collection: r.collection,
		filter:     filter,
		model:      r.model,
	}
}

func (r *MongoRepository[T]) WhereNotNull(field string) Query[T] {
	filter := bson.M{
		field: bson.M{
			"$ne": nil,
		},
	}
	return &MongoQuery[T]{
		collection: r.collection,
		filter:     filter,
		model:      r.model,
	}
}

func (r *MongoRepository[T]) WhereNull(field string) Query[T] {
	filter := bson.M{
		field: bson.M{
			"$eq": nil,
		},
	}
	return &MongoQuery[T]{
		collection: r.collection,
		filter:     filter,
		model:      r.model,
	}
}

func (r *MongoRepository[T]) With(relations string) Query[T] {
	// Log or ignore relations since MongoDB doesn't use them
	return &MongoQuery[T]{
		collection: r.collection,
		filter:     bson.M{},
		model:      r.model,
	}
}

func (r *MongoRepository[T]) WithTransaction(tx any) Repository[T] {
	sessionCtx, ok := tx.(mongo.SessionContext)
	if !ok {
		panic("invalid transaction context: must be mongo.SessionContext")
	}

	return &MongoRepository[T]{
		collection: r.collection,
		model:      r.model,
		session:    sessionCtx,
	}
}
