package stringutils

import (
	"strings"
)

// EnsureSuffix ensures that the string ends with the given suffix,
// adding the suffix IF AND ONLY IF it's not present already.
// EnsureSuffix 确保字符串以给定后缀结尾：当且仅当后缀不存在时才追加。
func EnsureSuffix(s, suffix string) string {
	if strings.HasSuffix(s, suffix) {
		return s
	}
	return s + suffix
}
