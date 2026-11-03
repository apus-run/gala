// Package stringbuilder wraps strings.Builder with helpers for
// space-prefixed and separator-joined writes.
// 包 stringbuilder 在 strings.Builder 之上提供带空格前缀、
// 带分隔符拼接等写入助手。
package stringbuilder

import (
	"io"
	"strings"
)

var _ io.Writer = new(stringBuilder)

type stringBuilder struct {
	builder *strings.Builder
}

// NewStringBuilder creates a new stringBuilder instance.
// NewStringBuilder 创建一个新的 stringBuilder 实例。
func NewStringBuilder() *stringBuilder {
	return &stringBuilder{
		builder: &strings.Builder{},
	}
}

// WriteLeadingString writes s to internal buffer.
// If it's not the first time to write the string, a blank (" ") will be written before s.
// WriteLeadingString 将 s 写入内部缓冲；若不是第一次写入，会先写入一个空格（" "）。
func (sb *stringBuilder) WriteLeadingString(s string) {
	if sb.builder.Len() > 0 {
		sb.builder.WriteString(" ")
	}

	sb.builder.WriteString(s)
}

// WriteString writes s verbatim | 原样写入 s.
func (sb *stringBuilder) WriteString(s string) {
	sb.builder.WriteString(s)
}

// WriteStrings joins the non-empty ss with sep and writes the result | 用 sep 拼接 ss 中的非空字符串并写入.
func (sb *stringBuilder) WriteStrings(ss []string, sep string) {
	if len(ss) == 0 {
		return
	}

	firstAdded := false
	if len(ss[0]) != 0 {
		sb.WriteString(ss[0])
		firstAdded = true
	}

	for _, s := range ss[1:] {
		if len(s) != 0 {
			if firstAdded {
				sb.WriteString(sep)
			}
			sb.WriteString(s)
			firstAdded = true
		}
	}
}

// WriteRune writes a single rune | 写入单个字符.
func (sb *stringBuilder) WriteRune(r rune) {
	sb.builder.WriteRune(r)
}

// Write implements io.Writer | 实现 io.Writer.
func (sb *stringBuilder) Write(data []byte) (int, error) {
	return sb.builder.Write(data)
}

// String returns the accumulated string | 返回已累积的字符串.
func (sb *stringBuilder) String() string {
	return sb.builder.String()
}

// Reset empties the builder | 清空构建器.
func (sb *stringBuilder) Reset() {
	sb.builder.Reset()
}

// Grow reserves n more bytes of capacity | 预留 n 字节的容量.
func (sb *stringBuilder) Grow(n int) {
	sb.builder.Grow(n)
}
