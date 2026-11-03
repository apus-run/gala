// Package types provides JSON/db-friendly value types: DateTime, JsonArray,
// JsonMap, JsonRaw and KV.
// 包 types 提供对 JSON 与数据库友好的值类型：DateTime、JsonArray、JsonMap、
// JsonRaw 和 KV。
package types

import (
	"database/sql/driver"
	"encoding/json"
	"time"

	"github.com/spf13/cast"
)

// DefaultDateLayout specifies the default app date strings layout.
// DefaultDateLayout 指定应用默认的日期字符串布局。
const DefaultDateLayout = "2006-01-02 15:04:05.000Z"

// NowDateTime returns new DateTime instance with the current local time.
// NowDateTime 返回持有当前本地时间的 DateTime 实例。
func NowDateTime() DateTime {
	return DateTime{t: time.Now()}
}

// ParseDateTime creates a new DateTime from the provided value
// (could be [cast.ToTime] supported string, [time.Time], etc.).
// ParseDateTime 用给定值创建 DateTime（可以是 [cast.ToTime] 支持的字符串、
// [time.Time] 等）。
func ParseDateTime(value any) (DateTime, error) {
	d := DateTime{}
	err := d.Scan(value)
	return d, err
}

// DateTime represents a [time.Time] instance in UTC that is wrapped
// and serialized using the app default date layout.
// DateTime 包装一个 [time.Time] 实例，按应用默认日期布局以 UTC 序列化。
type DateTime struct {
	t time.Time
}

// Time returns the internal [time.Time] instance.
// Time 返回内部的 [time.Time] 实例。
func (d DateTime) Time() time.Time {
	return d.t
}

// IsZero checks whether the current DateTime instance has zero time value.
// IsZero 判断当前 DateTime 的时间值是否为零值。
func (d DateTime) IsZero() bool {
	return d.Time().IsZero()
}

// String serializes the current DateTime instance into a formatted
// UTC date string.
//
// The zero value is serialized to an empty string.
// String 将当前 DateTime 序列化为格式化的 UTC 日期字符串；零值序列化为空字符串。
func (d DateTime) String() string {
	t := d.Time()
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(DefaultDateLayout)
}

// MarshalJSON implements the [json.Marshaler] interface.
// MarshalJSON 实现 [json.Marshaler] 接口。
func (d DateTime) MarshalJSON() ([]byte, error) {
	return []byte(`"` + d.String() + `"`), nil
}

// UnmarshalJSON implements the [json.Unmarshaler] interface.
// UnmarshalJSON 实现 [json.Unmarshaler] 接口。
func (d *DateTime) UnmarshalJSON(b []byte) error {
	var raw string
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	return d.Scan(raw)
}

// Value implements the [driver.Valuer] interface.
// Value 实现 [driver.Valuer] 接口。
func (d DateTime) Value() (driver.Value, error) {
	return d.String(), nil
}

// Scan implements [sql.Scanner] interface to scan the provided value
// into the current DateTime instance.
// Scan 实现 [sql.Scanner] 接口，将给定值扫描进当前 DateTime 实例。
func (d *DateTime) Scan(value any) error {
	switch v := value.(type) {
	case time.Time:
		d.t = v
	case DateTime:
		d.t = v.Time()
	case string:
		if v == "" {
			d.t = time.Time{}
		} else {
			t, err := time.Parse(DefaultDateLayout, v)
			if err != nil {
				// check for other common date layouts
				// 尝试其他常见日期布局
				t = cast.ToTime(v)
			}
			d.t = t
		}
	case int, int64, int32, uint, uint64, uint32:
		d.t = cast.ToTime(v)
	default:
		str := cast.ToString(v)
		if str == "" {
			d.t = time.Time{}
		} else {
			d.t = cast.ToTime(str)
		}
	}

	return nil
}
