package stringutils

import "strings"

// ConsumePrefix checks if *s has the given prefix, and if yes, modifies it
// to remove the prefix. The return value indicates whether the original string
// had the given prefix.
// ConsumePrefix 检查 *s 是否有给定前缀，有则原地移除该前缀；
// 返回值表示原字符串是否带有该前缀。
func ConsumePrefix(s *string, prefix string) bool {
	orig := *s
	rest, ok := strings.CutPrefix(orig, prefix)
	if !ok {
		return false
	}
	*s = rest
	return true
}

// ConsumeSuffix checks if *s has the given suffix, and if yes, modifies it
// to remove the suffix. The return value indicates whether the original string
// had the given suffix.
// ConsumeSuffix 检查 *s 是否有给定后缀，有则原地移除该后缀；
// 返回值表示原字符串是否带有该后缀。
func ConsumeSuffix(s *string, suffix string) bool {
	orig := *s
	rest, ok := strings.CutSuffix(orig, suffix)
	if !ok {
		return false
	}
	*s = rest
	return true
}
