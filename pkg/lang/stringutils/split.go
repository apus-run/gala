package stringutils

import "strings"

// Split2 splits the given string at the given separator, returning the part before and after the separator as two
// separate return values.
// If the string does not contain `sep`, the entire string is returned as the first return value.
// Split2 在第一个 sep 处切分字符串，返回分隔符前、后两部分；
// 字符串不含 sep 时，整个字符串作为第一个返回值。
func Split2(str string, sep string) (string, string) {
	before, after, ok := strings.Cut(str, sep)
	if !ok {
		return str, ""
	}
	return before, after
}

// Split2Last splits the given string at the last instance of the given separator,
// returning the part before and after the separator as two separate return values.
// If the string does not contain `sep`, the entire string is returned as the first return value.
// Split2Last 在最后一个 sep 处切分字符串，返回分隔符前、后两部分；
// 字符串不含 sep 时，整个字符串作为第一个返回值。
func Split2Last(str string, sep string) (string, string) {
	splitIdx := strings.LastIndex(str, sep)
	if splitIdx == -1 {
		return str, ""
	}
	return str[:splitIdx], str[splitIdx+len(sep):]
}

// SplitNPadded acts like `strings.SplitN`, but will *always* return a slice of length n (padded with empty strings
// if necessary).
// SplitNPadded 行为类似 strings.SplitN，但始终返回长度为 n 的切片（不足时用空串补齐）。
func SplitNPadded(str string, sep string, n int) []string {
	res := strings.SplitN(str, sep, n)
	for len(res) < n {
		res = append(res, "")
	}
	return res
}

// GetUpTo gets the values up to the separator or returns the input string if the separator does not exist
// GetUpTo 返回第一个 sep 之前的部分，不存在 sep 时返回原字符串。
func GetUpTo(str, sep string) string {
	part1, _ := Split2(str, sep)
	return part1
}

// GetAfter gets the substring after the separator or returns the input string if the separator does not exist
// GetAfter 返回第一个 sep 之后的部分，不存在 sep 时返回原字符串。
func GetAfter(str, sep string) string {
	p1, p2 := Split2(str, sep)
	if len(p1) == len(str) {
		return p1
	}
	return p2
}

// GetAfterLast gets the substring after the last instance of the given separator
// or returns the input string if the separator does not exist
// GetAfterLast 返回最后一个 sep 之后的部分，不存在 sep 时返回原字符串。
func GetAfterLast(str, sep string) string {
	p1, p2 := Split2Last(str, sep)
	if len(p1) == len(str) && sep != "" {
		return p1
	}
	return p2
}

// GetBetween gets the string between the two passed strings, otherwise it returns the empty string
// GetBetween 返回 start 与 end 之间的子串，找不到时返回空字符串。
func GetBetween(str, start, end string) string {
	startIdx := strings.Index(str, start)
	if startIdx == -1 || startIdx == len(str)-len(start) {
		return ""
	}
	offset := startIdx + len(start)
	endIdx := strings.Index(str[offset:], end)
	if endIdx == -1 {
		return ""
	}
	return str[offset : offset+endIdx]
}
