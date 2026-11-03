// Package sqlutil provides helper types for storing custom and JSON-typed
// values in SQL databases.
// 包 sqlutil 提供在 SQL 数据库中存取自定义类型与 JSON 类型值的助手类型。
package sqlutil

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// DriverValue wraps any type T as a driver.Valuer, mainly for using custom
// types in sqlx. For example:
// DriverValue 把任意类型 T 包装为 driver.Valuer，主要用于在 sqlx 中使用自定义类型。例如：
//
//	type myCustomType struct {
//	   Field1 string
//	   Field2 int
//	}
//
// sqlutil.DriverValue(myCustomType)
func DriverValue[T any](v T) driver.Valuer {
	return value[T]{val: v}
}

// value 是一个通用的 driver.Valuer 实现，它将任意类型 T 包装为 driver.Value。
// value is a generic driver.Valuer implementation wrapping any type T.
type value[T any] struct {
	val T
}

// Value returns a driver.Value | Value 返回一个 driver.Value.
func (i value[T]) Value() (driver.Value, error) {
	return i.val, nil
}

// JSONColumn represents a JSON-typed storage column, mainly for databases
// without a native json type.
// JSONColumn 代表存储字段的 json 类型，主要用于没有提供默认 json 类型的数据库。
//
// T can be a struct, a slice or a map; in theory anything the json library can
// handle. Using a pointer as T is discouraged: if T is a pointer, Valid MUST
// be false when Val is nil.
// T 可以是结构体、切片或 map，理论上一切能被 json 库处理的类型都可用作 T。
// 不建议使用指针作为 T 的类型；如果 T 是指针，在 Val 为 nil 时一定要把
// Valid 设置为 false。
type JSONColumn[T any] struct {
	// Val is the wrapped value of type T | 被包装的 T 类型值
	Val T
	// Valid indicates whether Val is valid (set after a successful Scan) | Val 是否有效（Scan 成功后置 true）
	Valid bool
}

// Value returns the JSON encoding of Val as a string.
// Value 返回 Val 的 JSON 串，类型是 string；无效时返回 nil。
func (j JSONColumn[T]) Value() (driver.Value, error) {
	if !j.Valid {
		return nil, nil
	}
	res, err := json.Marshal(j.Val)
	return string(res), err
}

// Scan converts src into the wrapped object.
// src must be []byte, string or nil; nil is left untouched.
// Scan 将 src 转化为对象。src 的类型必须是 []byte、string 或者 nil；
// 如果是 nil，则不做任何处理。
func (j *JSONColumn[T]) Scan(source any) error {
	var bs []byte
	switch val := source.(type) {
	case nil:
		return nil
	case []byte:
		bs = val
	case string:
		bs = []byte(val)
	default:
		return fmt.Errorf("JSONColumn.Scan 不支持 src 类型 %v", source)
	}

	if err := json.Unmarshal(bs, &j.Val); err != nil {
		return err
	}
	j.Valid = true
	return nil
}
