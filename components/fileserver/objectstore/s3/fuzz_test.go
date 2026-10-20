package s3

import (
	"strings"
	"testing"
)

// FuzzContentDisposition 检查生成的 Content-Disposition 头不会破坏 HTTP 头：
// 只含可打印 ASCII、不含换行，ASCII 回退名里的引号被替换，filename* 只含 attr-char 或百分号编码。
func FuzzContentDisposition(f *testing.F) {
	for _, seed := range []string{"a.txt", "报告.pdf", `a"b`, "a\\b", "a;b=c", "a\nb", "a\x00b", strings.Repeat("x", 300)} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, name string) {
		got, err := contentDisposition(name)
		if err != nil {
			return
		}
		for i := 0; i < len(got); i++ {
			if got[i] < 0x20 || got[i] > 0x7e {
				t.Fatalf("contentDisposition(%q) contains a non-printable byte: %q", name, got)
			}
		}
		if strings.Count(got, `"`) != 2 {
			t.Fatalf("contentDisposition(%q) = %q, want exactly the two quotes of the fallback", name, got)
		}
		_, encoded, ok := strings.Cut(got, "filename*=UTF-8''")
		if !ok {
			t.Fatalf("contentDisposition(%q) = %q, want the RFC 5987 form", name, got)
		}
		for i := 0; i < len(encoded); i++ {
			c := encoded[i]
			switch {
			case c == '%':
				if i+2 >= len(encoded) || !isHex(encoded[i+1]) || !isHex(encoded[i+2]) {
					t.Fatalf("contentDisposition(%q) has a broken percent escape: %q", name, encoded)
				}
				i += 2
			case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			case strings.IndexByte("!#$&+-.^_`|~", c) >= 0:
			default:
				t.Fatalf("contentDisposition(%q) has an unencoded byte %q in %q", name, c, encoded)
			}
		}
	})
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}
