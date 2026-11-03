// Package stringutils provides helpers for common string operations:
// emptiness checks, joining, splitting, prefix/suffix handling, truncation
// and wrapping.
// 包 stringutils 提供常见字符串操作的工具：空串判断、拼接、切分、前后缀处理、
// 截断与换行。
package stringutils

import (
	"fmt"
	"strings"
)

// WriteStrings writes the given strings to the builder, and ignores the error. (The documentation for Builder says it always
// returns a nil error.)
// WriteStrings 将给定字符串写入 builder 并忽略错误（Builder 文档说明其永远返回 nil 错误）。
func WriteStrings(sb *strings.Builder, strings ...string) {
	for _, s := range strings {
		_, _ = sb.WriteString(s)
	}
}

// WriteStringf writes the given formatted string to the builder, and ignores the error.
// WriteStringf 将格式化后的字符串写入 builder 并忽略错误。
func WriteStringf(sb *strings.Builder, format string, args ...any) {
	_, _ = sb.WriteString(fmt.Sprintf(format, args...))
}
