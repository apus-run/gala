package stringutils

import (
	"strings"

	"github.com/mitchellh/go-wordwrap"
)

// Wrap performs line-wrapping of the given text at 80 characters in length.
// Intended to be uses as a test template function.
// Wrap 以 80 字符宽度对给定文本换行（续行缩进 6 空格），
// 主要用作测试中的模板函数。
func Wrap(text string) string {
	wrapped := wordwrap.WrapString(text, 80)
	wrapped = strings.TrimSpace(wrapped)
	wrapped = strings.ReplaceAll(wrapped, "\n", "\n      ")
	return wrapped
}
