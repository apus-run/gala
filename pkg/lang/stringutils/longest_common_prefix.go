package stringutils

import "unicode/utf8"

// LongestCommonPrefix returns the longest common prefix of a and b, performing a byte-by-byte
// comparison. Note that this might yield invalid UTF-8 strings when applied to two valid UTF-8
// strings, as certain pairs of distinct Unicode characters share a common byte prefix in their
// UTF-8 encoding.
// LongestCommonPrefix 逐字节比较，返回 a 与 b 的最长公共前缀。注意：对两个合法 UTF-8
// 字符串也可能得到非法 UTF-8 结果，因为某些不同的 Unicode 字符在 UTF-8 编码下共享字节前缀。
func LongestCommonPrefix(a, b string) string {
	prefixLen := 0
	lenA, lenB := len(a), len(b)
	for prefixLen < lenA && prefixLen < lenB {
		if a[prefixLen] != b[prefixLen] {
			break
		}
		prefixLen++
	}
	return a[:prefixLen]
}

// LongestCommonPrefixUTF8 returns the longest common prefix of a and b, correctly handling
// UTF-8 encoded Unicode characters.
// LongestCommonPrefixUTF8 返回 a 与 b 的最长公共前缀，正确处理 UTF-8 编码的 Unicode 字符。
func LongestCommonPrefixUTF8(a, b string) string {
	prefixLen := 0 // length of prefix in _bytes_ | 前缀长度，以字节计
	lenA, lenB := len(a), len(b)
	for prefixLen < lenA && prefixLen < lenB {
		runeA, runeABytes := utf8.DecodeRuneInString(a[prefixLen:])
		runeB, _ := utf8.DecodeRuneInString(b[prefixLen:])
		if runeA != runeB {
			break
		}
		if runeA == utf8.RuneError {
			// fall back to byte comparison
			// 回退到字节比较
			if a[prefixLen] != b[prefixLen] {
				break
			}
			prefixLen++ // advance by 1 byte | 前进 1 个字节
		} else {
			prefixLen += runeABytes
		}
	}
	return a[:prefixLen]
}
