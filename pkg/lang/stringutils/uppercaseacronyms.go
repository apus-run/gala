package stringutils

import (
	"strings"
)

var acronymsToUpperCase = map[string]struct{}{
	"API":  {},
	"CPU":  {},
	"ID":   {},
	"UID":  {},
	"UUID": {},
}

// UpperCaseAcronyms returns the uppercase variant of the input if it is one of a specific set of acronyms
// (currently API, CPU, ID, UID, UUID).
// UpperCaseAcronyms 若输入属于特定缩写集合（目前为 API、CPU、ID、UID、UUID），
// 返回其大写形式，否则原样返回。
func UpperCaseAcronyms(s string) string {
	uc := strings.ToUpper(s)
	if _, found := acronymsToUpperCase[uc]; found {
		return uc
	}
	return s
}
