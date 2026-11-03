package stringutils

import (
	"fmt"
	"strings"
)

// TruncateOptions are the extra options to be passed when truncating a string
// TruncateOptions 是截断字符串时可附加的选项。
type TruncateOptions interface {
	process(s string) string
}

// WordOriented implies that the cut off will be the end of the last word instead of
// truncating in the middle of a word. If MaxCutOff is greater than zero, this determines
// the maximum number of characters that can be cut off, otherwise we'll relax the
// restriction of cutting off at a word boundary.
// WordOriented 表示截断点落在最后一个词的词尾而不是词中间。MaxCutOff 大于零时，
// 它限定最多可截掉的字符数；否则放宽“必须在词边界截断”的限制。
type WordOriented struct {
	// MaxCutOff is the maximum number of characters that can be cut off | 最多可截掉的字符数
	MaxCutOff int
}

func (w WordOriented) process(s string) string {
	// We'll need to search for a word break not from the end of the string but from three characters before the end of
	// the string, since we're going to append "...".
	// 搜索词边界时不是从字符串末尾开始，而是从末尾往前 3 个字符开始，因为末尾要追加 "..."。
	if len(s) < 3 {
		// If the length is shorter than 3, we cannot shorten it further since we're always appending '...' at the end.
		// 长度小于 3 时无法再缩短，因为末尾总要追加 '...'。
		return strings.TrimSpace(s)
	}

	idx := strings.LastIndex(s[:len(s)-2], " ")
	if idx == -1 || (w.MaxCutOff > 0 && idx < len(s)-w.MaxCutOff) {
		if len(s) > 3 {
			return fmt.Sprintf("%s...", s[:len(s)-3])
		}
		return s
	}

	trimmed := strings.TrimSpace(s[:idx])
	if trimmed == "" {
		return ""
	}
	return fmt.Sprintf("%s...", strings.TrimSpace(s[:idx]))
}

// Truncate truncates the string if necessary at maxlen.
// Truncate 在必要时把字符串截断到 maxLen 长度，可附带选项调整截断方式。
func Truncate(s string, maxLen int, options ...TruncateOptions) string {
	if len(s) <= maxLen {
		return s
	}
	s = s[:maxLen]
	for _, o := range options {
		s = o.process(s)
	}
	return s
}
