package stringutils

// OrDefault returns the string if it's not empty, or the default.
// OrDefault 在 s 非空时返回 s，否则返回默认值。
func OrDefault(s, defaultString string) string {
	if s != "" {
		return s
	}
	return defaultString
}

// PointerOrDefault returns the string if it's not nil nor empty, or the default.
// PointerOrDefault 在 s 非 nil 且非空时返回其指向的字符串，否则返回默认值。
func PointerOrDefault(s *string, defaultString string) string {
	if s == nil {
		return defaultString
	}

	return OrDefault(*s, defaultString)
}
