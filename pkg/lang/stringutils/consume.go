package stringutils

import "strings"

// ConsumePrefix checks if *s has the given prefix, and if yes, modifies it
// to remove the prefix. The return value indicates whether the original string
// had the given prefix.
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
func ConsumeSuffix(s *string, suffix string) bool {
	orig := *s
	rest, ok := strings.CutSuffix(orig, suffix)
	if !ok {
		return false
	}
	*s = rest
	return true
}
