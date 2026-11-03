package stringutils

import "slices"

// AllEmpty returns true if all the strings that are passed are empty
// AllEmpty 当所有传入字符串都为空时返回 true。
func AllEmpty(strs ...string) bool {
	for _, s := range strs {
		if s != "" {
			return false
		}
	}
	return true
}

// AllNotEmpty returns true if all the strings that are passed are not empty.
// AllNotEmpty 当所有传入字符串都非空时返回 true。
func AllNotEmpty(strs ...string) bool {
	return !slices.Contains(strs, "")
}

// AtLeastOneEmpty returns true if at least one of the strings is empty
// AtLeastOneEmpty 当至少有一个字符串为空时返回 true。
func AtLeastOneEmpty(strs ...string) bool {
	return slices.Contains(strs, "")
}

// FirstNonEmpty returns the first string that is non-empty in the variadic or returns an empty string
// FirstNonEmpty 返回第一个非空字符串，都没有则返回空字符串。
func FirstNonEmpty(strs ...string) string {
	for _, s := range strs {
		if s != "" {
			return s
		}
	}
	return ""
}

// LastNonEmpty returns the last string that is non-empty in the variadic or returns an empty string
// LastNonEmpty 返回最后一个非空字符串，都没有则返回空字符串。
func LastNonEmpty(strs ...string) string {
	if len(strs) == 0 {
		return ""
	}
	for _, str := range slices.Backward(strs) {
		if str != "" {
			return str
		}
	}
	return ""
}
