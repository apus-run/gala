package stringutils

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// ToString Change arg to string
// ToString 将 arg 转换为字符串；time.Time 可用 timeFormat 指定格式，指针会解引用，切片输出为 [a b c] 形式。
func ToString(arg any, timeFormat ...string) string {
	switch v := arg.(type) {
	case int:
		return strconv.Itoa(v)
	case int8:
		return strconv.FormatInt(int64(v), 10)
	case int16:
		return strconv.FormatInt(int64(v), 10)
	case int32:
		return strconv.FormatInt(int64(v), 10)
	case int64:
		return strconv.FormatInt(v, 10)
	case uint:
		return strconv.Itoa(int(v))
	case uint8:
		return strconv.FormatInt(int64(v), 10)
	case uint16:
		return strconv.FormatInt(int64(v), 10)
	case uint32:
		return strconv.FormatInt(int64(v), 10)
	case uint64:
		return strconv.FormatInt(int64(v), 10)
	case string:
		return v
	case []byte:
		return string(v)
	case bool:
		return strconv.FormatBool(v)
	case float32:
		return strconv.FormatFloat(float64(v), 'f', -1, 32)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case time.Time:
		if len(timeFormat) > 0 {
			return v.Format(timeFormat[0])
		}
		return v.Format("2006-01-02 15:04:05")
	case reflect.Value:
		return ToString(v.Interface(), timeFormat...)
	case fmt.Stringer:
		return v.String()
	default:
		// Check if the type is a pointer by using reflection
		// 用反射检查该类型是否为指针
		rv := reflect.ValueOf(arg)
		if rv.Kind() == reflect.Ptr && !rv.IsNil() {
			// Dereference the pointer and recursively call ToString
			// 解引用指针后递归调用 ToString
			return ToString(rv.Elem().Interface(), timeFormat...)
		} else if rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array {
			// handle slices
			// 处理切片
			var buf strings.Builder
			buf.WriteString("[") //nolint: revive,errcheck // no need to check error
			for i := range rv.Len() {
				if i > 0 {
					buf.WriteString(" ") //nolint: revive,errcheck // no need to check error
				}
				buf.WriteString(ToString(rv.Index(i).Interface())) //nolint: revive,errcheck // no need to check error
			}
			buf.WriteString("]") //nolint: revive,errcheck // no need to check error
			return buf.String()
		}

		// For types not explicitly handled, use fmt.Sprint to generate a string representation
		// 未显式处理的类型用 fmt.Sprint 生成字符串表示
		return fmt.Sprint(arg)
	}
}
