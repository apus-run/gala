package types

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// JsonMap defines a map that is safe for json and db read/write.
// JsonMap 定义可安全进行 JSON 与数据库读写的 map 类型。
type JsonMap map[string]any

// MarshalJSON implements the [json.Marshaler] interface.
// MarshalJSON 实现 [json.Marshaler] 接口。
func (m JsonMap) MarshalJSON() ([]byte, error) {
	type alias JsonMap // prevent recursion | 本地别名，防止递归

	// initialize an empty map to ensure that `{}` is returned as json
	// 初始化空 map，确保 JSON 输出为 `{}`
	if m == nil {
		m = JsonMap{}
	}

	return json.Marshal(alias(m))
}

// Get retrieves a single value from the current JsonMap.
//
// This helper was added primarily to assist the goja integration since custom map types
// don't have direct access to the map keys (https://pkg.go.dev/github.com/dop251/goja#hdr-Maps_with_methods).
// Get 从当前 JsonMap 取出单个值。该助手主要为 goja 集成而加：自定义 map 类型
// 无法直接访问 map 的 key（见上方链接）。
func (m JsonMap) Get(key string) any {
	return m[key]
}

// Set sets a single value in the current JsonMap.
//
// This helper was added primarily to assist the goja integration since custom map types
// don't have direct access to the map keys (https://pkg.go.dev/github.com/dop251/goja#hdr-Maps_with_methods).
// Set 在当前 JsonMap 中设置单个值。该助手主要为 goja 集成而加（原因同 Get）。
func (m JsonMap) Set(key string, value any) {
	m[key] = value
}

// Value implements the [driver.Valuer] interface.
// Value 实现 [driver.Valuer] 接口。
func (m JsonMap) Value() (driver.Value, error) {
	data, err := json.Marshal(m)

	return string(data), err
}

// Scan implements [sql.Scanner] interface to scan the provided value
// into the current `JsonMap` instance.
// Scan 实现 [sql.Scanner] 接口，将给定值扫描进当前 `JsonMap` 实例。
func (m *JsonMap) Scan(value any) error {
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
		return fmt.Errorf("failed to unmarshal JsonMap value: %q", value)
	}

	if len(data) == 0 {
		data = []byte("{}")
	}

	return json.Unmarshal(data, m)
}
