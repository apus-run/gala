package conv

import (
	"encoding/json"
	"strconv"
)

// StrToInt64 returns strconv.ParseInt(v, 10, 64) | StrToInt64 即 strconv.ParseInt(v, 10, 64).
func StrToInt64(v string) (int64, error) {
	return strconv.ParseInt(v, 10, 64)
}

// Int64ToStr returns strconv.FormatInt(v, 10) result | Int64ToStr 返回 strconv.FormatInt(v, 10) 的结果.
func Int64ToStr(v int64) string {
	return strconv.FormatInt(v, 10)
}

// StrToInt64D returns strconv.ParseInt(v, 10, 64)'s value.
// If error occurs, returns defaultValue as result.
// StrToInt64D 返回 strconv.ParseInt(v, 10, 64) 的值，出错时返回 defaultValue。
func StrToInt64D(v string, defaultValue int64) int64 {
	toV, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return defaultValue
	}
	return toV
}

// DebugJsonToStr marshals v to a JSON string for debugging; returns "" on
// marshal error.
// DebugJsonToStr 将 v 序列化为 JSON 字符串用于调试，序列化失败时返回 ""。
func DebugJsonToStr(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// BoolToInt maps true to 1 and false to 0 | BoolToInt 将 true 映射为 1，false 映射为 0.
func BoolToInt(p bool) int {
	if p == true {
		return 1
	}

	return 0
}

// BoolToIntPointer returns 1 or 0 as pointer, nil stays nil.
// BoolToIntPointer 以指针形式返回 1 或 0，入参为 nil 时返回 nil。
func BoolToIntPointer(p *bool) *int {
	if p == nil {
		return nil
	}

	if *p == true {
		return new(int(1))
	}

	return new(int(0))
}
