package slog

import (
	"log/slog"
	"time"
)

// ErrorKey is the attribute key used for error values | 错误值使用的属性键.
var ErrorKey = "error"

// ErrorString returns a string Attr for the error under ErrorKey | 以 ErrorKey 为键返回错误信息的字符串 Attr.
func ErrorString(err error) Attr {
	return slog.String(ErrorKey, err.Error())
}

// ErrorValue returns a string Value carrying the error message | 返回携带错误信息的字符串 Value.
func ErrorValue(err error) slog.Value {
	return slog.StringValue(err.Error())
}

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

// String returns a string Attr | 返回字符串 Attr.
func String(key, v string) Attr {
	return slog.String(key, v)
}

// Int64 returns an int64 Attr | 返回 int64 Attr.
func Int64(key string, v int64) Attr {
	return slog.Int64(key, v)
}

// Int returns an int Attr | 返回 int Attr.
func Int(key string, v int) Attr {
	return slog.Int(key, v)
}

// Uint64 returns an Attr for a uint64. | 返回 uint64 对应的 Attr。
func Uint64(key string, v uint64) Attr {
	return slog.Uint64(key, v)
}

// Float64 returns a float64 Attr | 返回 float64 Attr.
func Float64(key string, v float64) Attr {
	return slog.Float64(key, v)
}

// Bool returns a bool Attr | 返回布尔 Attr.
func Bool(key string, v bool) Attr {
	return slog.Bool(key, v)
}

// Time returns a time Attr | 返回时间 Attr.
func Time(key string, v time.Time) Attr {
	return slog.Time(key, v)
}

// Duration returns a duration Attr | 返回时长 Attr.
func Duration(key string, v time.Duration) Attr {
	return slog.Duration(key, v)
}

// Group returns a group Attr built from args | 用 args 构建分组 Attr.
func Group(key string, args ...any) Attr {
	return slog.Group(key, args...)
}

// Any returns an Attr holding v as-is | 返回原样携带 v 的 Attr.
func Any(key string, v any) Attr {
	return slog.Any(key, v)
}
