package stringutils

import (
	"strings"
)

// sanitizeString cleans up invalid characters
// sanitizeString 清理非法字符（无效 UTF-8 与 NUL）。
func sanitizeString(s string) string {
	s = strings.ToValidUTF8(s, "")
	return strings.ReplaceAll(s, "\x00", "")
}

// SanitizeMapValues cleans up invalid characters from a map to string
// SanitizeMapValues 原地清理 map 中所有字符串值的非法字符。
func SanitizeMapValues(m map[string]string) {
	for k, v := range m {
		m[k] = sanitizeString(v)
	}
}
