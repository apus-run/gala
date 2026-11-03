package mongo

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/bsoncodec"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type processor func(fn processFn) error
type processFn func(*cmd) error

type cmd struct {
	name     string
	req      []any
	res      any
	dbName   string
	collName string
}

func logCmd(logMode bool, c *cmd, name string, res any, req ...any) {
	// Only record req/res when log mode is on
	// 只有开启 log 模式才会记录 req、res
	if logMode {
		c.name = name
		c.req = append(c.req, req...)
		switch res := res.(type) {
		case *mongo.SingleResult:
			val, _ := res.Raw()
			c.res = val
		default:
			c.res = res
		}
	}
}

// Collection is a handle to a MongoDB collection | Collection 是 MongoDB 集合的句柄。
type Collection struct {
	collection *mongo.Collection

	registry *bsoncodec.Registry

	processor processor
	logMode   bool
}

func (c *Collection) cmd(cmd *cmd) *cmd {
	cmd.dbName = c.collection.Database().Name()
	cmd.collName = c.collection.Name()
	return cmd
}

// SetLogMode toggles command logging on this collection | SetLogMode 开关该 collection 的命令日志。
func (c *Collection) SetLogMode(logMode bool) {
	c.logMode = logMode
}

// Collection returns the underlying official collection handle
// | Collection 返回底层官方 collection 句柄。
func (c *Collection) Collection() *mongo.Collection {
	return c.collection
}

// DropCollection drops the collection. It is safe even when the collection
// does not exist.
// DropCollection 删除集合。集合不存在时也是安全的。
func (c *Collection) DropCollection(ctx context.Context) error {
	return c.processor(func(cmd *cmd) error {
		logCmd(c.logMode, c.cmd(cmd), "DropCollection", nil)
		return c.collection.Drop(ctx)
	})
}

// CloneCollection creates a copy of the Collection | CloneCollection 创建集合的副本。
func (c *Collection) CloneCollection() (coll *mongo.Collection, err error) {
	err = c.processor(func(cmd *cmd) error {
		coll, err = c.collection.Clone()
		logCmd(c.logMode, c.cmd(cmd), "CloneCollection", coll)

		return err
	})

	return coll, err

}

// GetCollectionName returns the collection name | GetCollectionName 返回集合名。
func (c *Collection) GetCollectionName() string {
	return c.collection.Name()
}

// Aggregate runs an aggregation pipeline | Aggregate 执行聚合管道。
func (c *Collection) Aggregate(ctx context.Context, pipe any, opts ...*options.AggregateOptions) *Cursor {
	var cur *mongo.Cursor
	var err error
	err = c.processor(func(cmd *cmd) error {
		cur, err = c.collection.Aggregate(ctx, pipe, opts...)
		logCmd(c.logMode, cmd, "Aggregate", cur, pipe, opts)

		return err
	})

	return &Cursor{
		ctx:    ctx,
		cursor: cur,
		err:    err,
	}
}

// Find runs a query and returns a Cursor | Find 执行查询并返回 Cursor。
func (c *Collection) Find(ctx context.Context, query any, opts ...*options.FindOptions) *Cursor {
	var cur *mongo.Cursor
	var err error

	err = c.processor(func(cmd *cmd) error {
		cur, err = c.collection.Find(ctx, query, opts...)
		logCmd(c.logMode, cmd, "Find", cur, query, opts)
		return err
	})

	return &Cursor{
		ctx:    ctx,
		cursor: cur,
		err:    err,
	}
}

// Count counts all documents in the collection | Count 统计集合中的全部文档数。
func (c *Collection) Count(ctx context.Context) (int, error) {
	num, err := c.collection.CountDocuments(ctx, struct{}{})
	return int(num), err
}

// FindOne returns the first document matching query | FindOne 返回匹配 query 的第一条文档。
func (c *Collection) FindOne(ctx context.Context, query any, opts ...*options.FindOneOptions) (sr *SingleResult) {
	_ = c.processor(func(cmd *cmd) error {
		sr = c.collection.FindOne(ctx, query, opts...)
		logCmd(c.logMode, c.cmd(cmd), "FindOne", sr, query, opts)

		return sr.Err()
	})

	return
}

// FindOneAndDelete deletes and returns the first match | FindOneAndDelete 删除并返回匹配的第一条文档。
func (c *Collection) FindOneAndDelete(ctx context.Context, query any, opts ...*options.FindOneAndDeleteOptions) (sr *SingleResult) {
	_ = c.processor(func(cmd *cmd) error {
		sr = c.collection.FindOneAndDelete(ctx, query, opts...)
		logCmd(c.logMode, c.cmd(cmd), "FindOneAndDelete", sr, query, opts)

		return sr.Err()
	})
	return
}

// FindOneAndReplace replaces and returns the first match | FindOneAndReplace 替换并返回匹配的第一条文档。
func (c *Collection) FindOneAndReplace(ctx context.Context, query, doc any, opts ...*options.FindOneAndReplaceOptions) (sr *SingleResult) {
	_ = c.processor(func(cmd *cmd) error {
		sr = c.collection.FindOneAndReplace(ctx, query, doc, opts...)
		logCmd(c.logMode, c.cmd(cmd), "FindOneAndReplace", sr, query, doc, opts)

		return sr.Err()
	})

	return
}

// FindOneAndUpdate updates and returns the first match | FindOneAndUpdate 更新并返回匹配的第一条文档。
func (c *Collection) FindOneAndUpdate(ctx context.Context, query, update any, opts ...*options.FindOneAndUpdateOptions) (sr *SingleResult) {
	_ = c.processor(func(cmd *cmd) error {
		sr = c.collection.FindOneAndUpdate(ctx, query, update, opts...)
		logCmd(c.logMode, c.cmd(cmd), "FindOneAndUpdate", sr, query, update, opts)

		return sr.Err()
	})

	return
}

// Indexes returns the index view of the collection | Indexes 返回集合的索引视图。
func (c *Collection) Indexes() mongo.IndexView { return c.collection.Indexes() }

// InsertMany inserts a slice of documents | InsertMany 插入一组文档（参数须为切片）。
func (c *Collection) InsertMany(ctx context.Context, docs []any, opts ...*options.InsertManyOptions) (imr *InsertManyResult, err error) {

	sDocs := interfaceToSliceInterface(docs)
	if sDocs == nil {
		return nil, errors.New("must be valid slice to insert")
	}

	err = c.processor(func(cmd *cmd) error {
		imr, err = c.collection.InsertMany(ctx, sDocs, opts...)
		logCmd(c.logMode, c.cmd(cmd), "InsertMany", imr, docs, opts)

		return err
	})

	return
}

// InsertOne inserts a single document | InsertOne 插入单条文档。
func (c *Collection) InsertOne(ctx context.Context, doc any, opts ...*options.InsertOneOptions) (ior *InsertOneResult, err error) {
	err = c.processor(func(cmd *cmd) error {
		ior, err = c.collection.InsertOne(ctx, doc, opts...)
		logCmd(c.logMode, c.cmd(cmd), "InsertOne", ior, doc, opts)
		return err
	})
	return
}

// ReplaceOne replaces the first match with doc | ReplaceOne 用 doc 替换匹配的第一条文档。
func (c *Collection) ReplaceOne(ctx context.Context, query, doc any, opts ...*options.ReplaceOptions) (ur *UpdateResult, err error) {
	err = c.processor(func(cmd *cmd) error {
		ur, err = c.collection.ReplaceOne(ctx, query, doc, opts...)
		logCmd(c.logMode, c.cmd(cmd), "ReplaceOne", ur, query, doc, opts)
		return err
	})

	return
}

// UpdateAll applies update to every match | UpdateAll 对所有匹配文档应用更新。
func (c *Collection) UpdateAll(ctx context.Context, query, update any, opts ...*options.UpdateOptions) (ur *UpdateResult, err error) {
	err = c.processor(func(cmd *cmd) error {
		ur, err = c.collection.UpdateMany(ctx, query, update, opts...)
		logCmd(c.logMode, c.cmd(cmd), "UpdateAll", ur, query, update, opts)
		return err
	})
	return
}

// UpdateMany applies update to every match | UpdateMany 对所有匹配文档应用更新。
func (c *Collection) UpdateMany(ctx context.Context, query, update any, opts ...*options.UpdateOptions) (ur *UpdateResult, err error) {
	err = c.processor(func(cmd *cmd) error {
		ur, err = c.collection.UpdateMany(ctx, query, update, opts...)
		logCmd(c.logMode, c.cmd(cmd), "UpdateMany", ur, query, update, opts)
		return err
	})
	return
}

// UpdateOne applies update to the first match | UpdateOne 对匹配的第一条文档应用更新。
func (c *Collection) UpdateOne(ctx context.Context, query, update any, opts ...*options.UpdateOptions) (ur *UpdateResult, err error) {
	err = c.processor(func(cmd *cmd) error {
		ur, err = c.collection.UpdateOne(ctx, query, update, opts...)
		logCmd(c.logMode, c.cmd(cmd), "UpdateOne", ur, query, update, opts)
		return err
	})

	return
}

// UpdateByID applies update to the document with the given _id
// | UpdateByID 对指定 _id 的文档应用更新。
func (c *Collection) UpdateByID(ctx context.Context, id any, update any, opts ...*options.UpdateOptions) (res *mongo.UpdateResult, err error) {
	_ = c.processor(func(cmd *cmd) error {
		res, err = c.collection.UpdateByID(ctx, id, update, opts...)
		logCmd(c.logMode, c.cmd(cmd), "UpdateByID", res, id, update)
		return err
	})
	return
}

// UpdateId applies update to the document with the given _id
// | UpdateId 对指定 _id 的文档应用更新。
func (c *Collection) UpdateId(ctx context.Context, id any, update any, opts ...*options.UpdateOptions) (ur *UpdateResult, err error) {
	err = c.processor(func(cmd *cmd) error {
		ur, err = c.collection.UpdateOne(ctx, bson.M{"_id": id}, update, opts...)
		logCmd(c.logMode, c.cmd(cmd), "UpdateId", ur, id, update, opts)
		return err
	})
	return
}

// DeleteMany deletes every match | DeleteMany 删除所有匹配文档。
func (c *Collection) DeleteMany(ctx context.Context, filter any, opts ...*options.DeleteOptions) (
	res *DeleteResult, err error) {

	err = c.processor(func(cmd *cmd) error {
		res, err = c.collection.DeleteMany(ctx, filter, opts...)
		logCmd(c.logMode, c.cmd(cmd), "DeleteMany", res, filter)
		return err
	})
	return
}

// DeleteOne deletes the first match | DeleteOne 删除匹配的第一条文档。
func (c *Collection) DeleteOne(ctx context.Context, filter any, opts ...*options.DeleteOptions) (dr *DeleteResult, err error) {
	err = c.processor(func(cmd *cmd) error {
		dr, err = c.collection.DeleteOne(ctx, filter, opts...)
		logCmd(c.logMode, c.cmd(cmd), "DeleteOne", dr, filter)
		return err
	})
	return
}

// Remove deletes every document matching q. It returns mongo.ErrNoDocuments
// when nothing matched, the same as RemoveId.
// Remove 删除所有匹配 q 的文档。没有匹配时返回 mongo.ErrNoDocuments，
// 与 RemoveId 一致。
func (c *Collection) Remove(ctx context.Context, q any) (err error) {
	var res *DeleteResult
	err = c.processor(func(cmd *cmd) error {
		res, err = c.collection.DeleteMany(ctx, q)
		logCmd(c.logMode, c.cmd(cmd), "Remove", res, q)
		return err
	})
	if err != nil {
		return err
	}
	if res != nil && res.DeletedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

// RemoveId deletes the document with the given _id, returning
// mongo.ErrNoDocuments when nothing matched
// | RemoveId 删除指定 _id 的文档，没有匹配时返回 mongo.ErrNoDocuments。
func (c *Collection) RemoveId(ctx context.Context, id any) error {
	res, err := c.collection.DeleteOne(ctx, bson.D{{Key: "_id", Value: id}})
	if res != nil && res.DeletedCount == 0 {
		err = mongo.ErrNoDocuments
	}
	if err != nil {
		return err
	}
	return err
}

// RemoveAll deletes every match and reports the removed count
// | RemoveAll 删除所有匹配文档并返回删除数量。
func (c *Collection) RemoveAll(ctx context.Context, q any) (*ChangeInfo, error) {
	var err error
	var res *DeleteResult
	err = c.processor(func(cmd *cmd) error {
		res, err = c.collection.DeleteMany(ctx, q)
		logCmd(c.logMode, c.cmd(cmd), "RemoveAll", res)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &ChangeInfo{Removed: int(res.DeletedCount)}, nil
}

// Distinct returns distinct values of fieldName | Distinct 返回 fieldName 的去重值。
func (c *Collection) Distinct(ctx context.Context, fieldName string, filter any, opts ...*options.DistinctOptions) (res []any, err error) {
	err = c.processor(func(cmd *cmd) error {
		res, err = c.collection.Distinct(ctx, fieldName, filter, opts...)
		logCmd(c.logMode, c.cmd(cmd), "Distinct", res, fieldName, filter)
		return err
	})
	return
}

// EstimatedDocumentCount returns a cheap estimate of the document count
// | EstimatedDocumentCount 返回低开销的文档数估计。
func (c *Collection) EstimatedDocumentCount(ctx context.Context, opts ...*options.EstimatedDocumentCountOptions) (res int64, err error) {
	err = c.processor(func(cmd *cmd) error {
		res, err = c.collection.EstimatedDocumentCount(ctx, opts...)
		logCmd(c.logMode, c.cmd(cmd), "EstimatedDocumentCount", res)
		return err
	})
	return
}

// Watch opens a change stream on the collection | Watch 在集合上打开变更流。
func (c *Collection) Watch(ctx context.Context, pipeline any, opts ...*options.ChangeStreamOptions) (res *mongo.ChangeStream, err error) {
	_ = c.processor(func(cmd *cmd) error {
		res, err = c.collection.Watch(ctx, pipeline, opts...)
		logCmd(c.logMode, c.cmd(cmd), "Watch", res, pipeline)
		return err
	})
	return
}

// ensureIndex creates multiple indexes on the collection and returns their
// names. Example: indexes = []string{"idx1", "-idx2", "idx3,idx4"} creates
// three indexes — idx1 ascending, idx2 descending, and idx3+idx4 as a
// compound ascending index.
// Reference: https://docs.mongodb.com/manual/reference/command/createIndexes/
// ensureIndex 在集合上创建多个索引并返回索引名。
// 示例：indexes = []string{"idx1", "-idx2", "idx3,idx4"} 会创建三个索引：
// idx1 升序、idx2 降序、idx3 与 idx4 组成的复合升序索引。
// 参考：https://docs.mongodb.com/manual/reference/command/createIndexes/
func (c *Collection) ensureIndex(ctx context.Context, indexes []IndexModel) error {
	var indexModels []mongo.IndexModel
	for _, idx := range indexes {
		var model mongo.IndexModel
		var keysDoc bson.D

		for _, field := range idx.Key {
			key, n := SplitSortField(field)

			keysDoc = append(keysDoc, bson.E{Key: key, Value: n})
		}
		model = mongo.IndexModel{
			Keys:    keysDoc,
			Options: idx.IndexOptions,
		}

		indexModels = append(indexModels, model)
	}

	if len(indexModels) == 0 {
		return nil
	}

	res, err := c.collection.Indexes().CreateMany(ctx, indexModels)
	if err != nil || len(res) == 0 {
		fmt.Println("<MongoDB.C>: ", c.collection.Name(), " Index: ", indexes, " error: ", err, "res: ", res)
		return err
	}
	return nil
}

// CreateIndexes creates multiple indexes in the collection.
// A Key of []string{"name"} creates the index: name.
// A Key of []string{"name","-age"} creates the compound index: name and -age.
// CreateIndexes 在集合上创建多个索引。
// Key 为 []string{"name"} 表示创建索引 name；
// Key 为 []string{"name","-age"} 表示创建复合索引 name 与 -age。
func (c *Collection) CreateIndexes(ctx context.Context, indexes []IndexModel) error {
	return c.ensureIndex(ctx, indexes)
}

// CreateOneIndex creates one index.
// A Key of []string{"name"} creates the index: name.
// A Key of []string{"name","-age"} creates the compound index: name and -age.
// CreateOneIndex 创建单个索引。
// Key 为 []string{"name"} 表示创建索引 name；
// Key 为 []string{"name","-age"} 表示创建复合索引 name 与 -age。
func (c *Collection) CreateOneIndex(ctx context.Context, index IndexModel) error {
	return c.ensureIndex(ctx, []IndexModel{index})
}

// DropAllIndexes drops all indexes on the collection except the index on the
// _id field; when only the _id index exists the call reports an error.
// DropAllIndexes 删除集合上除 _id 索引外的全部索引；
// 若集合只有 _id 索引，调用会报错。
func (c *Collection) DropAllIndexes(ctx context.Context) (err error) {
	_, err = c.collection.Indexes().DropAll(ctx)
	return err
}

// DropIndex drops indexes in the collection matching the input.
// indexes []string{"name"} drops the index: name.
// indexes []string{"name","-age"} drops the compound index: name and -age.
// DropIndex 删除集合中与输入一致的索引。
// indexes 为 []string{"name"} 表示删除索引 name；
// indexes 为 []string{"name","-age"} 表示删除复合索引 name 与 -age。
func (c *Collection) DropIndex(ctx context.Context, indexes []string) error {
	_, err := c.collection.Indexes().DropOne(ctx, generateDroppedIndex(indexes))
	if err != nil {
		return err
	}
	return err
}

// generateDroppedIndex builds the stored index name, which may join more than
// one index (like []string{"index1","index2"} stored as "index1_1_index2_1").
// generateDroppedIndex 生成存储在 mongo 中的索引名，可能由多个索引拼接
// （如 []string{"index1","index2"} 存为 "index1_1_index2_1"）。
func generateDroppedIndex(index []string) string {
	var res strings.Builder
	for _, e := range index {
		key, sort := SplitSortField(e)
		n := key + "_" + fmt.Sprint(sort)
		if res.Len() > 0 {
			res.WriteByte('_')
		}
		res.WriteString(n)
	}
	return res.String()
}

// interfaceToSliceInterface converts a slice value into []any
// | interfaceToSliceInterface 把切片值转换为 []any。
func interfaceToSliceInterface(docs any) []any {
	if reflect.Slice != reflect.TypeOf(docs).Kind() {
		return nil
	}
	s := reflect.ValueOf(docs)
	if s.Len() == 0 {
		return nil
	}
	var sDocs []any
	for i := range s.Len() {
		sDocs = append(sDocs, s.Index(i).Interface())
	}
	return sDocs
}
