package file_test

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	conf "github.com/apus-run/gala/components/conf/v2"
	"github.com/apus-run/gala/components/conf/v2/sources/file"
	yaml "gopkg.in/yaml.v3"
)

type shape struct {
	Server struct {
		Port int `conf:"port"`
	} `conf:"server"`
	Name string `conf:"name"`
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
		dec := yaml.Unmarshal
		if name == "a.json" {
			dec = file.JSON
		}
		c, err := conf.Open(t.Context(), shape{}, file.New(filepath.Join(dir, name), dec))
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

// Count reads at the filesystem boundary, including when a read fails.
type countingFS struct {
	fstest.MapFS
	reads int
	err   error
}

func (fsys *countingFS) ReadFile(name string) ([]byte, error) {
	fsys.reads++
	if fsys.err != nil {
		return nil, fsys.err
	}
	return fsys.MapFS.ReadFile(name)
}

func TestReadsOncePerLoad(t *testing.T) {
	fsys := &countingFS{MapFS: fstest.MapFS{"config": {Data: []byte(`{"name":"first"}`)}}}
	cfg, err := conf.Open(t.Context(), shape{}, file.FS(fsys, "config", file.JSON))
	if err != nil || fsys.reads != 1 {
		t.Fatalf("Open: reads=%d err=%v", fsys.reads, err)
	}
	old := cfg.Current()
	fsys.err = &fs.PathError{Op: "open", Path: "SECRET", Err: fs.ErrPermission}
	err = cfg.Reload(t.Context())
	if !errors.Is(err, fs.ErrPermission) || fsys.reads != 2 || strings.Contains(err.Error(), "SECRET") || cfg.Current() != old {
		t.Fatalf("failed reload: reads=%d err=%v", fsys.reads, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = file.FS(fsys, "config", file.JSON).Load(ctx)
	if !errors.Is(err, context.Canceled) || fsys.reads != 2 {
		t.Fatal(err, fsys.reads)
	}
}

// Error text must not expose the full path or raw codec text.
// 错误文本不得暴露完整路径或原始 codec 文本。
func TestSafeErrorText(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bad.yaml")
	write(t, p, "server: [unclosed\n")

	_, err := conf.Open(t.Context(), shape{}, file.New(p, yaml.Unmarshal))
	if err == nil {
		t.Fatal("want decode error")
	}
	if msg := err.Error(); strings.Contains(msg, dir) {
		t.Fatalf("error exposes directory: %q", msg)
	}
	ce, ok := errors.AsType[*conf.Error](err)
	if !ok || ce.Stage != conf.StageLoad {
		t.Fatalf("stage = %v, want load (err: %v)", ce, err)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestFileErrorOmitsUntrustedName(t *testing.T) {
	p := filepath.Join(t.TempDir(), "missing.yaml")
	_, err := conf.Open(t.Context(), shape{}, file.New(p, yaml.Unmarshal))
	ce, ok := errors.AsType[*conf.Error](err)
	if !ok || !errors.Is(err, fs.ErrNotExist) || ce.Source != 0 {
		t.Fatalf("file identity: %#v", err)
	}
}

type openFunc func(string) (fs.File, error)

func (open openFunc) Open(name string) (fs.File, error) { return open(name) }

func TestFSTypedNilOpener(t *testing.T) {
	var open openFunc
	if _, err := file.FS(open, "config", file.JSON).Load(t.Context()); err == nil {
		t.Fatal("expected invalid file source")
	}
}

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

			dec := yaml.Unmarshal
			if filepath.Ext(p) == ".json" {
				dec = file.JSON
			}
			_, err := conf.Open(t.Context(), shape{}, file.New(p, dec))
			if err == nil {
				t.Fatal("malformed input: want error, got nil")
			}
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("message leaks secret: %q", err.Error())
			}
			if cause := errors.Unwrap(err); cause != nil && strings.Contains(cause.Error(), secret) {
				t.Fatalf("unwrapped cause leaks secret: %q", cause.Error())
			}
			ce, ok := errors.AsType[*conf.Error](err)
			if !ok || ce.Stage != conf.StageLoad {
				t.Fatalf("stage != decode: %v", err)
			}
		})
	}
}

func TestJSONDocument(t *testing.T) {
	const number = "9007199254740993"
	var v any
	if err := file.JSON([]byte(number), &v); err != nil || v != json.Number(number) {
		t.Fatal(v, err)
	}
	for _, raw := range []string{"1 2", "{} x"} {
		if file.JSON([]byte(raw), &v) == nil {
			t.Fatal(raw)
		}
	}
	fsys := fstest.MapFS{"config": {Data: []byte(`{"N":9007199254740993}`)}}
	cfg, err := conf.Open(t.Context(), struct{ N int64 }{}, file.FS(fsys, "config", file.JSON))
	if err != nil || cfg.Current().N != 9007199254740993 {
		t.Fatal(err)
	}
	_, err = file.FS(fsys, "missing", file.JSON).Load(t.Context())
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
}
