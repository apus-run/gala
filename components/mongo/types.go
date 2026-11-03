package mongo

import (
	"errors"
	"io"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// InsertOneResult is an alias of the driver's InsertOneResult | InsertOneResult 是驱动 InsertOneResult 的别名.
type InsertOneResult = mongo.InsertOneResult

// InsertManyResult is an alias of the driver's InsertManyResult | InsertManyResult 是驱动 InsertManyResult 的别名.
type InsertManyResult = mongo.InsertManyResult

// UpdateResult is an alias of the driver's UpdateResult | UpdateResult 是驱动 UpdateResult 的别名.
type UpdateResult = mongo.UpdateResult

// SingleResult is an alias of the driver's SingleResult | SingleResult 是驱动 SingleResult 的别名.
type SingleResult = mongo.SingleResult

// DeleteResult is an alias of the driver's DeleteResult | DeleteResult 是驱动 DeleteResult 的别名.
type DeleteResult = mongo.DeleteResult

// IndexModel describes an index to create | IndexModel 描述要创建的索引。
type IndexModel struct {
	// Key lists index key fields; prefix a name with dash (-) for descending
	// order | 索引键字段；名字前加减号（-）表示降序
	Key []string
	*options.IndexOptions
}

// ChangeInfo represents the data returned by Update and Upsert documents.
// This type mirrors the mgo type.
// ChangeInfo 表示 Update 与 Upsert 返回的数据，镜像 mgo 的同名类型。
type ChangeInfo struct {
	// Updated counts existing documents updated | 被更新的已有文档数
	Updated int
	// Removed counts documents removed | 被删除的文档数
	Removed int
	// UpsertedId is the upserted _id field, when not explicitly provided | 未显式提供 _id 时插入得到的 _id
	UpsertedId any
}

// Change represents the options that you can pass to the findAndModify
// operation.
// Change 表示传给 findAndModify 操作的选项。
type Change struct {
	// Update is the update document | 更新文档
	Update any
	// Upsert inserts when the document is not found | 文档不存在时是否插入
	Upsert bool
	// Remove removes the found document instead of updating | 删除查到的文档而不是更新
	Remove bool
	// ReturnNew returns the modified document rather than the old one | 返回修改后的文档而不是旧文档
	ReturnNew bool
}

// KeyValue is one key/value pair used by D | KeyValue 是 D 使用的键值对。
type KeyValue struct {
	// Key is the element name | 元素名.
	Key string
	// Value is the element value | 元素值.
	Value any
}

// KV builds a KeyValue | KV 构造 KeyValue。
func KV(key string, value any) KeyValue {
	return KeyValue{Key: key, Value: value}
}

// M builds a single-entry bson.M | M 构造单键 bson.M。
func M(key string, value any) bson.M {
	return bson.M{key: value}
}

// E builds a bson.E element | E 构造 bson.E 元素。
func E(key string, value any) bson.E {
	return bson.E{Key: key, Value: value}
}

// A builds a bson.A from values | A 由 values 构造 bson.A。
func A[T any](values ...T) bson.A {
	value := make(bson.A, 0, len(values))
	for _, v := range values {
		value = append(value, v)
	}
	return value
}

// D builds a bson.D from KeyValue pairs | D 由 KeyValue 序列构造 bson.D。
func D(bsonElements ...KeyValue) bson.D {
	value := make(bson.D, 0, len(bsonElements))
	for _, element := range bsonElements {
		value = append(value, bson.E{Key: element.Key, Value: element.Value})
	}
	return value
}

// ID builds an _id filter for value | ID 构造按 _id 查询 value 的过滤器。
func ID(value any) bson.M {
	return M("_id", value)
}

// ToUpdate passes a map through as an update document | ToUpdate 把 map 原样作为更新文档。
func ToUpdate(vals map[string]any) bson.M {
	return vals
}

// ToFilter converts a map to a bson.D filter | ToFilter 把 map 转成 bson.D 过滤器。
func ToFilter(vals map[string]any) bson.D {
	var res bson.D
	for k, v := range vals {
		res = append(res, bson.E{k, v})
	}
	return res
}

// Set builds a $set update document from vals | Set 用 vals 构造 $set 更新文档。
func Set(vals map[string]any) bson.M {
	return bson.M{"$set": bson.M(vals)}
}

// GetSort converts keys like "-field" into a bson.D sort document
// | GetSort 把形如 "-field" 的键转成 bson.D 排序文档。
func GetSort(keys []string) bson.D {
	if len(keys) == 0 {
		return nil
	}

	sort := bson.D{}

	for _, k := range keys {
		if field, ok := strings.CutPrefix(k, "-"); ok {
			sort = append(sort, bson.E{Key: field, Value: -1})
		} else if field, ok := strings.CutPrefix(k, "+"); ok {
			sort = append(sort, bson.E{Key: field, Value: 1})
		} else {
			sort = append(sort, bson.E{Key: k, Value: 1})
		}
	}

	return sort
}

// PrepSort prepares sort params for the mongo driver and returns bson.D.
// Input strings are provided as [+|-]field1,[+|-]field2,[+|-]field3...
// + means ascending, - means descending; a missing prefix means ascending.
// PrepSort 为 mongo 驱动准备排序参数并返回 bson.D。
// 输入形如 [+|-]field1,[+|-]field2,[+|-]field3...：+ 升序、- 降序，
// 缺省前缀视为升序。
func PrepSort(sort ...string) bson.D {
	res := bson.D{}
	for _, s := range sort {
		if s == "" {
			continue
		}
		s = strings.TrimSpace(s)
		switch s[0] {
		case '-':
			res = append(res, bson.E{Key: s[1:], Value: -1})
		case '+':
			res = append(res, bson.E{Key: s[1:], Value: 1})
		default:
			res = append(res, bson.E{Key: s, Value: 1})
		}
	}
	return res
}

// PrepIndex prepares index params for the mongo driver and returns an
// IndexModel.
// PrepIndex 为 mongo 驱动准备索引参数并返回 IndexModel。
func PrepIndex(keys ...string) mongo.IndexModel {
	return mongo.IndexModel{Keys: PrepSort(keys...)}
}

// Bind decodes a request JSON body from an io.Reader into a bson record.
// Bind 把请求 JSON body 从 io.Reader 解码为 bson 记录。
func Bind(r io.Reader, v any) error {
	body, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	return bson.UnmarshalExtJSON(body, false, v)
}

// GetFindAndModifyReturn maps returnNew to the driver's ReturnDocument
// | GetFindAndModifyReturn 把 returnNew 映射为驱动的 ReturnDocument。
func GetFindAndModifyReturn(returnNew bool) options.ReturnDocument {
	if returnNew {
		return options.After
	}
	return options.Before
}

// IsErrNoDocuments checks whether err is mongo.ErrNoDocuments
// | IsErrNoDocuments 判断 err 是否为 no documents 错误。
func IsErrNoDocuments(err error) bool {
	return errors.Is(err, mongo.ErrNoDocuments)
}

// IsDup checks whether err is a mongo E11000 duplicate error.
// IsDup 判断 err 是否为 mongo E11000 重复键错误。
func IsDup(err error) bool {
	return err != nil && strings.Contains(err.Error(), "E11000")
}

// SplitSortField handles the sort symbol "+" or "-" in front of a field:
// "+" returns sort as 1, "-" returns sort as -1.
// SplitSortField 处理字段前的 "+"、"-" 排序符号：
// "+" 返回 1，"-" 返回 -1。
func SplitSortField(field string) (key string, sort int32) {
	sort = 1
	key = field

	if len(field) != 0 {
		switch field[0] {
		case '+':
			key = strings.TrimPrefix(field, "+")
			sort = 1
		case '-':
			key = strings.TrimPrefix(field, "-")
			sort = -1
		}
	}

	return key, sort
}

// Now returns the current time truncated to milliseconds | Now 返回截断到毫秒的当前时间。
func Now() time.Time {
	return time.Unix(0, time.Now().UnixNano()/1e6*1e6)
}

// NewObjectID generates a new ObjectID. Watch out: the way it generates the
// objectID is different from mgo.
// NewObjectID 生成新的 ObjectID。注意：生成方式与 mgo 不同。
func NewObjectID() primitive.ObjectID {
	return primitive.NewObjectID()
}
