package file

import "strings"

// Format returns the part of name after the last dot (the extension), or ""
// when name has no dot | Format 返回 name 最后一个点之后的部分（扩展名），
// 没有点时返回 ""。
func Format(name string) string {
	if p := strings.Split(name, "."); len(p) > 1 {
		return p[len(p)-1]
	}
	return ""
}
