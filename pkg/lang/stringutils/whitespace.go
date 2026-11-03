package stringutils

import (
	"strings"
	"unicode"
)

// ContainsWhitespace returns whether the string contains any whitespace characters.
// ContainsWhitespace 判断字符串是否含有空白字符。
func ContainsWhitespace(s string) bool {
	return strings.LastIndexFunc(s, unicode.IsSpace) != -1
}
