package zlog

import (
	"time"

	"go.uber.org/zap"
)

// TimeValue returns a Value for a time.Time.
// It discards the monotonic portion.
// TimeValue 返回 time.Time 对应的 Value，会丢弃单调时钟部分。
func TimeValue(v time.Time) any {
	return uint64(v.UnixNano())
}

// DurationValue returns a Value for a time.Duration. | 返回 time.Duration 对应的 Value。
func DurationValue(v time.Duration) uint64 {
	return uint64(v.Nanoseconds())
}

// Err returns a field carrying the error | 返回携带错误的字段.
func Err(err error) Field {
	return zap.Error(err)
}

// String returns a string field | 返回字符串字段.
func String(key, v string) Field {
	return zap.String(key, v)
}

// Uint64 returns a uint64 field | 返回 uint64 字段.
func Uint64(key string, v uint64) Field {
	return zap.Uint64(key, v)
}

// Int64 returns an int64 field | 返回 int64 字段.
func Int64(key string, v int64) Field {
	return zap.Int64(key, v)
}

// Float64 returns a float64 field | 返回 float64 字段.
func Float64(key string, v float64) Field {
	return zap.Float64(key, v)
}

// Bool returns a bool field | 返回布尔字段.
func Bool(key string, b bool) Field {
	return zap.Bool(key, b)
}

// Int returns an int field, stored as int64 | 返回 int 字段（以 int64 存储）.
func Int(key string, v int) Field {
	return Int64(key, int64(v))
}

// Any returns a field holding v with its inferred type | 返回按 v 的实际类型编码的字段.
func Any(key string, v any) Field {
	return zap.Any(key, v)
}

// Time returns a time field | 返回时间字段.
func Time(key string, v time.Time) Field {
	return zap.Time(key, v)
}

// Duration returns a duration field | 返回时长字段.
func Duration(key string, v time.Duration) Field {
	return zap.Duration(key, v)
}
