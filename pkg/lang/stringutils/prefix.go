package stringutils

import "strings"

// MaybeTrimPrefix trims the prefix from the given input string, if it exists as a prefix.
// MaybeTrimPrefix 若 s 带有 prefix 则去掉它；返回剩余字符串及是否去掉了前缀。
func MaybeTrimPrefix(s, prefix string) (string, bool) {
	return strings.CutPrefix(s, prefix)
}
