package types

import (
	"database/sql/driver"
	"encoding/json"
)

// KV is a string-keyed map that can be scanned from and written to a
// database as a JSON string.
// KV 是字符串键的 map，可作为 JSON 字符串进行数据库扫描与写入。
type KV map[string]any

// Scan implements sql.Scanner to unmarshal a JSON string into the map.
// Scan 实现 sql.Scanner，把 JSON 字符串反序列化进 map。
func (kv KV) Scan(value any) error {
	return json.Unmarshal([]byte(value.(string)), &kv)
}

// Value implements driver.Valuer, marshaling the map to a JSON string
// ("{}" when empty).
// Value 实现 driver.Valuer，把 map 序列化为 JSON 字符串（空 map 输出 "{}"）。
func (kv KV) Value() (driver.Value, error) {
	if len(kv) == 0 {
		return "{}", nil
	}
	b, err := json.Marshal(kv)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}
