package fileserver

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	// MaxKeyLength 是 Key 的最大字节数（UTF-8 编码后）。
	MaxKeyLength = 1024
	// MaxSegmentLength 是 Key 中单个路径段的最大字节数。
	MaxSegmentLength = 255
)

// forbiddenKeyChars 是 Windows 文件名不允许的字符（'/' 是段分隔符，单独处理）。
// 所有后端统一拒绝它们，使同一个 Key 在任何平台上都能落盘。
const forbiddenKeyChars = `\:*?"<>|`

// ValidateKey 按 Key 规则检查 key（见 doc.go）。失败时返回的错误满足
// errors.Is(err, ErrInvalidKey)，且错误文本不回显 key。
func ValidateKey(key string) error {
	if reason := keyViolation(key); reason != "" {
		return fmt.Errorf("%w: %s", ErrInvalidKey, reason)
	}
	return nil
}

func keyViolation(key string) string {
	switch {
	case key == "":
		return "empty"
	case len(key) > MaxKeyLength:
		return "longer than 1024 bytes"
	case !utf8.ValidString(key):
		return "not valid UTF-8"
	case strings.ContainsAny(key, forbiddenKeyChars):
		return "contains a character that Windows forbids in file names"
	case hasControl(key):
		return "contains control character"
	case key[0] == '/' || key[len(key)-1] == '/':
		return "leading or trailing '/'"
	}
	for _, seg := range strings.Split(key, "/") {
		if reason := segmentViolation(seg); reason != "" {
			return reason
		}
	}
	return ""
}

func segmentViolation(seg string) string {
	switch {
	case seg == "":
		return "empty segment"
	case seg == "." || seg == "..":
		return "relative segment"
	case len(seg) > MaxSegmentLength:
		return "segment longer than 255 bytes"
	case seg[len(seg)-1] == ' ' || seg[len(seg)-1] == '.':
		return "segment ends with space or '.'"
	case isReservedWindowsName(seg):
		return "reserved Windows device name"
	}
	return ""
}

// isReservedWindowsName 判断段名去掉扩展名后是否为 Windows 保留设备名。
func isReservedWindowsName(seg string) bool {
	base, _, _ := strings.Cut(seg, ".")
	base = strings.ToUpper(base)
	switch base {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}
	if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) {
		return base[3] >= '0' && base[3] <= '9'
	}
	return false
}

// hasControl 报告 s 是否包含 ASCII 控制字符（0x00–0x1F、0x7F）。
func hasControl(s string) bool {
	for i := range len(s) {
		if c := s[i]; c < 0x20 || c == 0x7f {
			return true
		}
	}
	return false
}
