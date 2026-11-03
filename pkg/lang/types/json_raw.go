package types

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
)

// JsonRaw defines a json value type that is safe for db read/write.
// JsonRaw 定义可安全进行数据库读写的 JSON 值类型。
type JsonRaw []byte

// ParseJsonRaw creates a new JsonRaw instance from the provided value
// (could be JsonRaw, int, float, string, []byte, etc.).
// ParseJsonRaw 用给定值创建新的 JsonRaw 实例（可为 JsonRaw、int、float、
// string、[]byte 等）。
func ParseJsonRaw(value any) (JsonRaw, error) {
	result := JsonRaw{}
	err := result.Scan(value)
	return result, err
}

// String returns the current JsonRaw instance as a json encoded string.
// String 以 JSON 编码字符串形式返回当前 JsonRaw 实例。
func (j JsonRaw) String() string {
	return string(j)
}

// MarshalJSON implements the [json.Marshaler] interface.
// MarshalJSON 实现 [json.Marshaler] 接口。
func (j JsonRaw) MarshalJSON() ([]byte, error) {
	if len(j) == 0 {
		return []byte("null"), nil
	}

	return j, nil
}

// UnmarshalJSON implements the [json.Unmarshaler] interface.
// UnmarshalJSON 实现 [json.Unmarshaler] 接口。
func (j *JsonRaw) UnmarshalJSON(b []byte) error {
	if j == nil {
		return errors.New("JsonRaw: UnmarshalJSON on nil pointer")
	}

	*j = append((*j)[0:0], b...)

	return nil
}

// Value implements the [driver.Valuer] interface.
// Value 实现 [driver.Valuer] 接口。
func (j JsonRaw) Value() (driver.Value, error) {
	if len(j) == 0 {
		return nil, nil
	}

	return j.String(), nil
}

// Scan implements [sql.Scanner] interface to scan the provided value
// into the current JsonRaw instance.
// Scan 实现 [sql.Scanner] 接口，将给定值扫描进当前 JsonRaw 实例。
func (j *JsonRaw) Scan(value any) error {
	var data []byte

	switch v := value.(type) {
	case nil:
		// no cast is needed
		// 无需转换
	case []byte:
		if len(v) != 0 {
			data = v
		}
	case string:
		if v != "" {
			data = []byte(v)
		}
	case JsonRaw:
		if len(v) != 0 {
			data = []byte(v)
		}
	default:
		bytes, err := json.Marshal(v)
		if err != nil {
			return err
		}
		data = bytes
	}

	return j.UnmarshalJSON(data)
}
