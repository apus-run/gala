package types

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// JsonArray defines a slice that is safe for json and db read/write.
// JsonArray 定义可安全进行 JSON 与数据库读写的切片类型。
type JsonArray[T any] []T

// internal alias to prevent recursion during marshalization.
// 内部别名，防止序列化时递归。
type jsonArrayAlias[T any] JsonArray[T]

// MarshalJSON implements the [json.Marshaler] interface.
// MarshalJSON 实现 [json.Marshaler] 接口。
func (m JsonArray[T]) MarshalJSON() ([]byte, error) {
	// initialize an empty map to ensure that `[]` is returned as json
	// 初始化空切片，确保 JSON 输出为 `[]`
	if m == nil {
		m = JsonArray[T]{}
	}

	return json.Marshal(jsonArrayAlias[T](m))
}

// Value implements the [driver.Valuer] interface.
// Value 实现 [driver.Valuer] 接口。
func (m JsonArray[T]) Value() (driver.Value, error) {
	data, err := json.Marshal(m)

	return string(data), err
}

// Scan implements [sql.Scanner] interface to scan the provided value
// into the current JsonArray[T] instance.
// Scan 实现 [sql.Scanner] 接口，将给定值扫描进当前 JsonArray[T] 实例。
func (m *JsonArray[T]) Scan(value any) error {
	var data []byte
	switch v := value.(type) {
	case nil:
		// no cast needed
		// 无需转换
	case []byte:
		data = v
	case string:
		data = []byte(v)
	default:
		return fmt.Errorf("failed to unmarshal JsonArray value: %q", value)
	}

	if len(data) == 0 {
		data = []byte("[]")
	}

	return json.Unmarshal(data, m)
}
