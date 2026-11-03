package stringutils

import "slices"

// MatchesAny returns true if the value matches any of the options
// MatchesAny 当 value 等于任意一个选项时返回 true。
func MatchesAny(value string, options ...string) bool {
	return slices.Contains(options, value)
}
