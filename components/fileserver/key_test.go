package fileserver

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestValidateKeyAccepts(t *testing.T) {
	segments := []string{
		strings.Repeat("a", 255), strings.Repeat("a", 255), strings.Repeat("a", 255), strings.Repeat("a", 254), "x",
	}
	valid := []string{
		"a",
		"a/b",
		"tenant/2026/10/0f3c-uuid",
		"文件/名称.txt",
		"a.b.c",
		"a-b_c~d",
		"dir/.hidden",
		"x..y",
		"a b/c d",
		"CONSOLE",
		"COMX/file",
		"file.con",
		strings.Repeat("a", 255),
		strings.Join(segments, "/"),
	}
	for _, key := range valid {
		if err := ValidateKey(key); err != nil {
			t.Errorf("ValidateKey(%q) = %v, want nil", key, err)
		}
	}
}

func TestValidateKeyRejects(t *testing.T) {
	invalid := []string{
		"",
		"/a",
		"a/",
		"a//b",
		"a/./b",
		"a/../b",
		"..",
		".",
		`a\b`,
		"a:b",
		"a*b",
		"a?b",
		`a"b`,
		"a<b",
		"a>b",
		"a|b",
		"dir/x|y",
		"a\x00b",
		"a\x1fb",
		"a\x7fb",
		"\xff",
		"a/\xc3",
		"dir/CON",
		"CON",
		"nul.txt",
		"dir/trailing.",
		"...",
		"dir/trailing ",
		"dir/com1",
		"dir/LPT9.log",
		strings.Repeat("a", 256),
		strings.Repeat("a", 1025),
		strings.Join([]string{strings.Repeat("a", 255), strings.Repeat("a", 255), strings.Repeat("a", 255), strings.Repeat("a", 255), "x"}, "/"),
	}
	for _, key := range invalid {
		err := ValidateKey(key)
		if !errors.Is(err, ErrInvalidKey) {
			t.Errorf("ValidateKey(%q) = %v, want ErrInvalidKey", key, err)
		}
	}
}

func TestValidateKeyErrorDoesNotEchoKey(t *testing.T) {
	err := ValidateKey("secret/../../etc")
	if err == nil {
		t.Fatal("ValidateKey accepted a traversal key")
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("error %q echoes the key", err)
	}
}

// FuzzValidateKey 检查接受的 Key 满足全部不变量，且被拒绝的错误都满足 ErrInvalidKey。
func FuzzValidateKey(f *testing.F) {
	for _, seed := range []string{"a", "a/b", "../x", "CON.txt", ".fs-x/y", "a\x00", "x/", strings.Repeat("k", 300)} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, key string) {
		err := ValidateKey(key)
		if err != nil {
			if !errors.Is(err, ErrInvalidKey) {
				t.Fatalf("ValidateKey(%q) = %v, want ErrInvalidKey", key, err)
			}
			return
		}
		if key == "" || len(key) > MaxKeyLength || !utf8.ValidString(key) {
			t.Fatalf("accepted key with bad length or encoding: %q", key)
		}
		if key[0] == '/' || key[len(key)-1] == '/' || strings.ContainsAny(key, `\:*?"<>|`) || hasControl(key) {
			t.Fatalf("accepted key with forbidden characters: %q", key)
		}
		for _, seg := range strings.Split(key, "/") {
			last := seg[len(seg)-1]
			if seg == "" || seg == "." || seg == ".." || len(seg) > MaxSegmentLength || last == ' ' || last == '.' {
				t.Fatalf("accepted key with bad segment %q in %q", seg, key)
			}
			if isReservedWindowsName(seg) {
				t.Fatalf("accepted reserved Windows name %q in %q", seg, key)
			}
		}
	})
}
