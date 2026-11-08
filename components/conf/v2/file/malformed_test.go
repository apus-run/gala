package file_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	conf "github.com/apus-run/gala/components/conf/v2"
	"github.com/apus-run/gala/components/conf/v2/file"
)

// C08 (malformed input): a syntactically broken file that embeds a secret must
// fail in the decode stage, and neither the message nor the unwrapped cause may
// contain the secret. The codec's own error text often echoes input, so this
// checks that it is dropped.
// C08（畸形输入）：嵌入密钥的语法错误文件必须在 decode 阶段失败，错误消息与
// Unwrap 得到的原因都不得含密钥。codec 自身的错误文本常回显输入，此处验证其被丢弃。
func TestC08MalformedInputHidesSecret(t *testing.T) {
	const secret = "TESTKEY-9f3a-yaml"
	cases := map[string]struct {
		name string
		body string
	}{
		"yaml unclosed flow":   {"bad.yaml", "server: [" + secret + "\n"},
		"yaml bad indentation": {"bad2.yaml", "server:\n port: 1\n  token: " + secret + "\n bad: [\n"},
		"json truncated":       {"bad.json", `{"server":{"token":"` + secret},
		"json bad syntax":      {"bad2.json", `{"token": ` + secret + `}`},
	}
	dir := t.TempDir()
	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			p := filepath.Join(dir, tc.name)
			write(t, p, tc.body)

			_, err := conf.Open[shape](context.Background(), file.New(p))
			if err == nil {
				t.Fatal("malformed input: want error, got nil")
			}
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("message leaks secret: %q", err.Error())
			}
			if cause := errors.Unwrap(err); cause != nil && strings.Contains(cause.Error(), secret) {
				t.Fatalf("unwrapped cause leaks secret: %q", cause.Error())
			}
			var ce *conf.Error
			if !errors.As(err, &ce) || ce.Stage != conf.StageDecode {
				t.Fatalf("stage != decode: %v", err)
			}
		})
	}
}
