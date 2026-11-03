package logger

import (
	"log/slog"
	"time"
)

// ErrorString returns the error as a string attr under "error"
// | ErrorString 以 "error" 键返回字符串属性。
func ErrorString(err error) slog.Attr {
	return slog.String("error", err.Error())
}

// ErrorValue returns the error as a string Value | ErrorValue 以字符串 Value 返回错误。
func ErrorValue(err error) slog.Value {
	return slog.StringValue(err.Error())
}

// TimeValue returns a Value for a time.Time.
// It discards the monotonic portion.
// TimeValue 返回 time.Time 对应的 Value，丢弃单调时钟部分。
func TimeValue(v time.Time) any {
	return uint64(v.UnixNano())
}

// DurationValue returns a Value for a time.Duration.
// DurationValue 返回 time.Duration 对应的 Value。
func DurationValue(v time.Duration) uint64 {
	return uint64(v.Nanoseconds())
}

// String returns a string attr | String 返回字符串属性。
func String(key, v string) slog.Attr {
	return slog.String(key, v)
}

// Int64 returns an int64 attr | Int64 返回 int64 属性。
func Int64(key string, v int64) slog.Attr {
	return slog.Int64(key, v)
}

// Int returns an int attr | Int 返回 int 属性。
func Int(key string, v int) slog.Attr {
	return slog.Int(key, v)
}

// Uint64 returns an Attr for a uint64.
// Uint64 返回 uint64 属性。
func Uint64(key string, v uint64) slog.Attr {
	return slog.Uint64(key, v)
}

// Float64 returns a float64 attr | Float64 返回 float64 属性。
func Float64(key string, v float64) slog.Attr {
	return slog.Float64(key, v)
}

// Bool returns a bool attr | Bool 返回布尔属性。
func Bool(key string, v bool) slog.Attr {
	return slog.Bool(key, v)
}

// Time returns a time attr | Time 返回时间属性。
func Time(key string, v time.Time) slog.Attr {
	return slog.Time(key, v)
}

// Duration returns a duration attr | Duration 返回时长属性。
func Duration(key string, v time.Duration) slog.Attr {
	return slog.Duration(key, v)
}

// Group returns a group attr | Group 返回分组属性。
func Group(key string, args ...any) slog.Attr {
	return slog.Group(key, args...)
}

// Any returns an attr of the inferred type | Any 按推断类型返回属性。
func Any(key string, v any) slog.Attr {
	return slog.Any(key, v)
}
