package file_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/apus-run/gala/pkg/encoding/json"
	_ "github.com/apus-run/gala/pkg/encoding/yaml"

	conf "github.com/apus-run/gala/components/conf/v2"
	"github.com/apus-run/gala/components/conf/v2/file"
)

type shape struct {
	Server struct {
		Port int `mapstructure:"port"`
	} `mapstructure:"server"`
	Name string `mapstructure:"name"`
}

// C07: .json, .yaml, and .yml with the same content produce the same config.
// C07：内容相同的 .json、.yaml、.yml 得到相同的配置。
func TestC07FormatsAgree(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.json"), `{"server":{"port":8080},"name":"x"}`)
	write(t, filepath.Join(dir, "a.yaml"), "server:\n  port: 8080\nname: x\n")
	write(t, filepath.Join(dir, "a.yml"), "server:\n  port: 8080\nname: x\n")

	var got []shape
	for _, name := range []string{"a.json", "a.yaml", "a.yml"} {
		c, err := conf.Open[shape](t.Context(), file.New(filepath.Join(dir, name)))
		if err != nil {
			t.Fatalf("%s: open: %v", name, err)
		}
		got = append(got, *c.Current())
	}
	// shape is comparable, so a direct comparison beats reflect.DeepEqual.
	// shape 可直接比较，无需 reflect.DeepEqual。
	for i := 1; i < len(got); i++ {
		if got[0] != got[i] {
			t.Fatalf("format %d differs: %+v vs %+v", i, got[0], got[i])
		}
	}
}

// C07: a missing file is reported as not-exist via errors.Is, and an
// unknown extension is an explicit error.
// C07：缺文件可用 errors.Is 判定为不存在；未知扩展名明确报错。
func TestC07MissingFileAndUnknownExt(t *testing.T) {
	dir := t.TempDir()

	_, err := conf.Open[shape](t.Context(), file.New(filepath.Join(dir, "nope.yaml")))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing file: errors.Is(ErrNotExist) = false, err = %v", err)
	}

	write(t, filepath.Join(dir, "c.toml"), "name = \"x\"\n")
	if _, err := conf.Open[shape](t.Context(), file.New(filepath.Join(dir, "c.toml"))); err == nil {
		t.Fatal("unknown extension: want error, got nil")
	}
}

// C07: one Open calls Load exactly once per source.
// C07：一次 Open 对每个来源恰好调用一次 Load。
func TestC07ReadsOnce(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "r.yaml")
	write(t, p, "name: x\n")

	counted := &countingSource{inner: file.New(p)}
	if _, err := conf.Open[shape](t.Context(), counted); err != nil {
		t.Fatalf("open: %v", err)
	}
	if counted.calls != 1 {
		t.Fatalf("Load calls = %d, want 1", counted.calls)
	}
}

// Error text must not expose the full path or raw codec text.
// 错误文本不得暴露完整路径或原始 codec 文本。
func TestSafeErrorText(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bad.yaml")
	write(t, p, "server: [unclosed\n")

	_, err := conf.Open[shape](t.Context(), file.New(p))
	if err == nil {
		t.Fatal("want decode error")
	}
	if msg := err.Error(); strings.Contains(msg, dir) {
		t.Fatalf("error exposes directory: %q", msg)
	}
	var ce *conf.Error
	if !errors.As(err, &ce) || ce.Stage != conf.StageDecode {
		t.Fatalf("stage = %v, want decode (err: %v)", ce, err)
	}
}

// countingSource counts Load calls on an inner source.
// countingSource 统计内部来源的 Load 次数。
type countingSource struct {
	inner *file.Source
	calls int
}

func (c *countingSource) Load(ctx context.Context) (map[string]any, error) {
	c.calls++
	return c.inner.Load(ctx)
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestFileErrorRetainsSafeName(t *testing.T) {
	p := filepath.Join(t.TempDir(), "missing.yaml")
	_, err := conf.Open[shape](t.Context(), file.New(p))
	var ce *conf.Error
	if !errors.As(err, &ce) || ce.Name != "missing.yaml" || ce.Source != 0 {
		t.Fatalf("file identity: %#v", err)
	}
}

func TestActualCodecsIntegerPrecision(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		valid      bool
	}{
		{"large.json", `{"server":{"port":9007199254740993}}`, false},
		{"large.yaml", "server:\n  port: 9007199254740993\n", true},
		{"empty.yaml", " \n", false},
		{"list.yaml", "- value\n", false},
		{"null.json", "null", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), tc.name)
			write(t, p, tc.body)
			c, err := conf.Open[shape](t.Context(), file.New(p))
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v: %v", tc.valid, err)
			}
			if tc.valid && c.Current().Server.Port != 9007199254740993 {
				t.Fatal("precision lost")
			}
		})
	}
}
