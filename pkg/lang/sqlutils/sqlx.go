package sqlutil

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// DriverValue 用于将任意类型转换为 driver.Valuer
// 主要用于在 sqlx 中使用自定义类型
// 例如：
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

// value 是一个通用的 driver.Valuer 实现
// 它将任意类型 T 包装为 driver.Value
type value[T any] struct {
	val T
}

// Value 返回一个 driver.Value
func (i value[T]) Value() (driver.Value, error) {
	return i.val, nil
}

// JSONColumn 代表存储字段的 json 类型
// 主要用于没有提供默认 json 类型的数据库
// T 可以是结构体，也可以是切片或者 map
// 理论上来说一切可以被 json 库所处理的类型都能被用作 T
// 不建议使用指针作为 T 的类型
// 如果 T 是指针，那么在 Val 为 nil 的情况下，一定要把 Valid 设置为 false
type JSONColumn[T any] struct {
	Val   T
	Valid bool
}

// Value 返回一个 json 串。类型是 string
func (j JSONColumn[T]) Value() (driver.Value, error) {
	if !j.Valid {
		return nil, nil
	}
	res, err := json.Marshal(j.Val)
	return string(res), err
}

// Scan 将 src 转化为对象
// src 的类型必须是 []byte, string 或者 nil
// 如果是 nil，我们不会做任何处理
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
