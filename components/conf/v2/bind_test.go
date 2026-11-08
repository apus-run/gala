package conf_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	conf "github.com/apus-run/gala/components/conf/v2"
	"github.com/apus-run/gala/components/conf/v2/env"
)

type typed struct {
	Port    int           `mapstructure:"port"`
	N8      int8          `mapstructure:"n8"`
	Debug   bool          `mapstructure:"debug"`
	Timeout time.Duration `mapstructure:"timeout"`
	Name    string        `mapstructure:"name"`
	Tags    []string      `mapstructure:"tags"`
	Nested  struct {
		Level int `mapstructure:"level"`
	} `mapstructure:"nested"`
}

// C05: nested objects, empty lists, and explicit zero values survive; ENV unset
// and ENV empty both leave the file value in place.
// C05：层级对象、空列表与显式零值保留；ENV 未设置与设为空串都不覆盖文件值。
func TestC05ZeroValuesAndEnvAbsence(t *testing.T) {
	file := &fakeSource{data: map[string]any{
		"port":   8080,
		"debug":  false,
		"tags":   []any{},
		"nested": map[string]any{"level": 0},
	}}
	c, err := conf.Open[typed](t.Context(), file)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	got := c.Current()
	if got.Port != 8080 || got.Debug || len(got.Tags) != 0 || got.Nested.Level != 0 {
		t.Fatalf("zero values not preserved: %+v", *got)
	}
}

// C05 (ENV): a variable set to "" is absent, so it does not override the file.
// This is the env Source's contract, so the real env source is used here.
// C05（ENV）：设为空串的变量视为缺席，不覆盖文件。这是 env 来源的契约，
// 因此这里使用真实的 env 来源。
func TestC05EnvEmptyDoesNotOverride(t *testing.T) {
	t.Setenv("TEST_CONF_NAME_EMPTY", "")
	file := &fakeSource{data: map[string]any{"name": "from-file"}}
	c, err := conf.Open[typed](t.Context(), file, env.New(map[string]string{
		"TEST_CONF_NAME_EMPTY": "name",
	}))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if got := c.Current().Name; got != "from-file" {
		t.Fatalf("name = %q, want from-file", got)
	}
}

// C06a: ENV-style strings bind to int, bool, and Duration; a file int binds too.
// C06a：ENV 形式的字符串能绑定到 int、bool 与 Duration；文件中的整数也能绑定。
func TestC06aEnvStringsBind(t *testing.T) {
	env := &fakeSource{data: map[string]any{"port": "9000", "debug": "true", "timeout": "500ms"}}
	c, err := conf.Open[typed](t.Context(), env)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	got := c.Current()
	if got.Port != 9000 || !got.Debug || got.Timeout != 500*time.Millisecond {
		t.Fatalf("env strings not bound: %+v", *got)
	}
}

// C06: each bad input must fail with an error that exposes no test secret, and
// none may silently become a wrong value.
// C06：每个错误输入都必须报错，且错误不含测试密钥，也不得静默得到错误值。
func TestC06RejectsBadInputs(t *testing.T) {
	const secret = "TESTKEY-9f3a"
	cases := []struct {
		name string
		data map[string]any
	}{
		{"int8 overflow int", map[string]any{"n8": 300}},
		{"int8 overflow string", map[string]any{"n8": "300"}},
		{"float fraction into int", map[string]any{"port": 8080.5}},
		{"non-numeric string into int", map[string]any{"port": "9000x"}},
		{"float at 2^53", map[string]any{"port": float64(1 << 53)}},
		{"bare number as duration", map[string]any{"timeout": 500}},
		{"bare string as duration", map[string]any{"timeout": "500"}},
		{"yes as bool", map[string]any{"debug": "yes"}},
		{"unknown key", map[string]any{"prot": "1", "port": 1}},
		{"secret-bearing bad value", map[string]any{"name": 12, "port": secret}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := conf.Open[typed](t.Context(), &fakeSource{data: tc.data})
			if err == nil {
				t.Fatalf("want error, got nil")
			}
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("error leaks secret: %q", err.Error())
			}
		})
	}
}

// Business Validate errors are exposed through Unwrap and Error() by design.
// 业务 Validate 的错误按设计经 Unwrap 与 Error() 暴露。
func TestValidateErrorIsExposed(t *testing.T) {
	_, err := conf.Open[appConfig](t.Context(), &fakeSource{data: portFile(0)})
	if err == nil {
		t.Fatal("want validate error")
	}
	var ce *conf.Error
	if !errors.As(err, &ce) || ce.Stage != conf.StageValidate {
		t.Fatalf("stage != validate: %v", err)
	}
	if !strings.Contains(err.Error(), "server.port") {
		t.Fatalf("Error() omits validate text: %q", err.Error())
	}
	if errors.Unwrap(err) == nil {
		t.Fatal("Unwrap returned nil for validate error")
	}
}

// C06 (values): valid integer strings and in-range numbers still bind exactly.
// C06（值）：合法的整数字符串与范围内的数字仍精确绑定。
func TestC06AcceptsExactValues(t *testing.T) {
	c, err := conf.Open[typed](t.Context(), &fakeSource{data: map[string]any{
		"port": 8080,
		"n8":   127,
	}})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if got := c.Current(); got.Port != 8080 || got.N8 != 127 {
		t.Fatalf("exact values = %+v", *got)
	}
}
