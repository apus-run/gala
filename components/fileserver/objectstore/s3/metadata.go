package s3

import (
	"fmt"
	"strings"

	"github.com/apus-run/gala/components/fileserver"
)

// validateMetadata 检查用户元数据是否满足 S3 的限制：键必须是 HTTP 头的 token 字符，
// 值必须是可见 ASCII，键与值的总字节数不得超过 2 KiB。键已由根包统一小写。
func validateMetadata(m map[string]string) error {
	total := 0
	for k, v := range m {
		if !isHeaderToken(k) {
			return invalidArgument("metadata key contains characters that S3 does not allow")
		}
		if !isPrintableASCIIOrSpace(v) {
			return invalidArgument("metadata value must be printable ASCII")
		}
		total += len(k) + len(v)
	}
	if total > maxMetadataBytes {
		return fmt.Errorf("%w: metadata exceeds %d bytes", fileserver.ErrInvalidArgument, maxMetadataBytes)
	}
	return nil
}

// validateContentType 要求内容类型可以放进 HTTP 头：可见 ASCII 或空格。
func validateContentType(s string) error {
	if s != "" && !isPrintableASCIIOrSpace(s) {
		return invalidArgument("content type must be printable ASCII")
	}
	return nil
}

// normalizeMetadata 返回键已小写的副本；没有元数据时返回 nil。
func normalizeMetadata(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[strings.ToLower(k)] = v
	}
	return out
}

// isHeaderToken 判断 s 是否只含 RFC 7230 的 token 字符。
func isHeaderToken(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0:
		default:
			return false
		}
	}
	return true
}

// isPrintableASCIIOrSpace 判断 s 是否只含可见 ASCII 字符与空格，可以放进 HTTP 头。
func isPrintableASCIIOrSpace(s string) bool {
	for i := range len(s) {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return true
}
