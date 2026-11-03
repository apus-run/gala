// Package regex compiles regexes into matchers that only accept strings
// matching the whole pattern.
// 包 regex 把正则编译为“整串匹配”的匹配器。
package regex

import (
	"fmt"
	"regexp"

	"github.com/pkg/errors"
)

// A WholeStringMatcher is something that can match a string against a regex, but only matches
// strings where the entire string matches the regex.
// WholeStringMatcher 可用正则匹配字符串，但只有整串完全匹配时才算命中。
type WholeStringMatcher interface {
	// MatchWholeString reports whether s matches the whole pattern | 报告 s 是否整串匹配该模式.
	MatchWholeString(s string) bool
}

// Flags represents regex flags.
// See the Flags table in https://github.com/google/re2/wiki/Syntax for the list
// of regex flags.
// For simplicity, we have not implemented all flags here, only what
// we have used, but we are using a struct to enable seamlessly adding more.
// Flags 表示正则标志，完整列表见 https://github.com/google/re2/wiki/Syntax。
// 为简单起见这里只实现了用到的标志，采用结构体以便后续无缝扩充。
type Flags struct {
	// CaseInsensitive makes matching case-insensitive | 匹配时忽略大小写
	CaseInsensitive bool
}

// regexRepr represents these flags in the regexp format.
// See the Flags table in https://github.com/google/re2/wiki/Syntax.
// regexRepr 以 regexp 格式表示这些标志。
func (f *Flags) regexRepr() string {
	if f.CaseInsensitive {
		return "i"
	}
	return ""
}

// CompileWholeStringMatcher takes a regex and compiles it into a WholeStringMatcher.
// An empty regex matches _all_ strings.
// CompileWholeStringMatcher 把正则编译为 WholeStringMatcher；空正则匹配所有字符串。
func CompileWholeStringMatcher(re string, flags Flags) (WholeStringMatcher, error) {
	if re == "" {
		return &wholeStringMatcher{}, nil
	}
	compiled, err := regexp.Compile(fmt.Sprintf("^(?%s:%s)$", flags.regexRepr(), re))
	if err != nil {
		return nil, errors.Wrap(err, "invalid regex")
	}
	return &wholeStringMatcher{r: compiled}, nil
}

type wholeStringMatcher struct {
	r *regexp.Regexp
}

// MatchWholeString implements WholeStringMatcher | 实现 WholeStringMatcher.
func (w *wholeStringMatcher) MatchWholeString(s string) bool {
	return w.r == nil || w.r.MatchString(s)
}
