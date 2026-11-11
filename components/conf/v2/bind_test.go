package conf_test

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"math"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	conf "github.com/apus-run/gala/components/conf/v2"
	"github.com/apus-run/gala/components/conf/v2/sources/env"
	"github.com/apus-run/gala/components/conf/v2/sources/file"
	"github.com/apus-run/gala/components/conf/v2/sources/flagset"
)

type typed struct {
	Port    int           `conf:"port"`
	N8      int8          `conf:"n8"`
	Debug   bool          `conf:"debug"`
	Timeout time.Duration `conf:"timeout"`
	Name    string        `conf:"name"`
	Tags    []string      `conf:"tags"`
	Nested  struct {
		Level int `conf:"level"`
	} `conf:"nested"`
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
	c, err := conf.Open(t.Context(), typed{}, file)
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
	c, err := conf.Open(t.Context(), typed{}, file, env.New(map[string]string{
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
	c, err := conf.Open(t.Context(), typed{}, env)
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
			_, err := conf.Open(t.Context(), typed{}, &fakeSource{data: tc.data})
			if err == nil {
				t.Fatalf("want error, got nil")
			}
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("error leaks secret: %q", err.Error())
			}
		})
	}
}

// C06 (values): valid integer strings and in-range numbers still bind exactly.
// C06（值）：合法的整数字符串与范围内的数字仍精确绑定。
func TestC06AcceptsExactValues(t *testing.T) {
	c, err := conf.Open(t.Context(), typed{}, &fakeSource{data: map[string]any{
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

func TestIntegerKindsCannotBypassChecks(t *testing.T) {
	for _, value := range []any{int16(300), int32(300), float32(300), float32(1.5), uint(300), uint64(300), math.NaN(), math.Inf(1)} {
		if _, err := conf.Open(t.Context(), typed{}, &fakeSource{data: map[string]any{"n8": value}}); err == nil {
			t.Errorf("accepted %T %v", value, value)
		}
	}
	for _, value := range []any{int8(-128), int16(127), int32(127), int64(127), float32(127), float64(127), "-128"} {
		if _, err := conf.Open(t.Context(), typed{}, &fakeSource{data: map[string]any{"n8": value}}); err != nil {
			t.Errorf("rejected %T: %v", value, err)
		}
	}
}

func scalar[T any](t *testing.T, input any, want T, reason string) {
	t.Helper()
	c, err := conf.Open(t.Context(), struct{ Value T }{}, &inputSource{data: map[string]any{"Value": input}})
	if reason != "" {
		var ce *conf.Error
		if !errors.As(err, &ce) || ce.Stage != conf.StageBind || ce.Path != "Value" || ce.Err.Error() != reason {
			t.Errorf("%T(%v) -> %T: %v, want %s", input, input, want, err, reason)
		}
	} else if err != nil || !reflect.DeepEqual(c.Current().Value, want) {
		t.Errorf("%T(%v) -> %T: %v %v", input, input, want, c, err)
	}
}

func TestNumericTargets(t *testing.T) {
	scalar(t, json.Number("9007199254740993"), int64(9007199254740993), "")
	scalar(t, json.Number("9007199254740993"), uint64(9007199254740993), "")
	scalar(t, json.Number("9007199254740993"), float64(9007199254740992), "")
	scalar(t, "16777217", float32(16777216), "")
	scalar(t, json.Number("18446744073709551615"), uint64(math.MaxUint64), "")
	scalar(t, "18446744073709551616", uint64(0), "value out of range")
	scalar(t, int64(-1), uint32(0), "value out of range")
	scalar(t, float32(1<<24), int64(0), "value out of range")
	scalar(t, float64(1<<53), uint64(0), "value out of range")
	scalar(t, json.Number("1e3"), int64(0), "not an exact integer")
	scalar(t, json.Number("1.0"), uint(0), "not an exact integer")
	scalar(t, json.Number("01"), float64(0), "type mismatch")
	scalar(t, "3.5e39", float32(0), "value out of range")
	scalar(t, "-1e-999", float64(0), "")
	scalar(t, math.Inf(1), float32(0), "type mismatch")
	scalar(t, "0x1p2", float32(0), "type mismatch")
	scalar(t, "+.5e+1", float32(5), "")
	scalar(t, json.Number("1"), "", "string required; quote string values")
	scalar(t, json.Number("1"), false, "type mismatch")
	scalar(t, json.Number("1"), time.Duration(0), "duration needs an explicit unit")
}

func invalidType[T any](t *testing.T, def T, path string) {
	t.Helper()
	src := &inputSource{}
	c, err := conf.Open(t.Context(), def, src)
	var ce *conf.Error
	if c != nil || src.reads != 0 || !errors.As(err, &ce) || ce.Stage != conf.StageConfig || ce.Path != path {
		t.Errorf("invalid %T: %v (reads %d)", def, err, src.reads)
	}
}

func TestTypePreflight(t *testing.T) {
	type node struct{ Next *node }
	type tree struct{ Children []tree }
	type dict map[string]dict
	type Base struct{ V int }
	invalidType(t, node{}, "Next")
	invalidType(t, tree{}, "Children")
	invalidType(t, struct{ M dict }{}, "M")
	invalidType(t, struct{ Bad any }{}, "Bad")
	invalidType(t, struct{ Bad uintptr }{}, "Bad")
	invalidType(t, struct{ Bad complex64 }{}, "Bad")
	invalidType(t, struct{ Bad chan int }{}, "Bad")
	invalidType(t, struct{ Bad func() }{}, "Bad")
	invalidType(t, struct{ Bad map[int]bool }{}, "Bad")
	invalidType(t, struct{ Bad **int }{}, "Bad")
	invalidType(t, struct {
		A, B int `conf:"x"`
	}{}, "x")
	invalidType(t, struct {
		A int `conf:"a,omit"`
	}{}, "A")
	invalidType(t, struct{ a int }{}, "")
	type Key string
	_, err := conf.Open(t.Context(), struct {
		A, B   Base
		M      map[Key][]*Base
		Ignore any `conf:"-"`
	}{}, &inputSource{data: map[string]any{"M": map[string]any{"x": []any{map[string]int{"V": 2}}}}})
	if err != nil {
		t.Fatal(err)
	}
}

type secretText struct{ private string }

func (s *secretText) UnmarshalText(raw []byte) error {
	s.private = string(raw)
	return fmt.Errorf("SECRET %s", raw)
}
func TestTextBinding(t *testing.T) {
	type Base struct{ Count int }
	type Config struct {
		Base
		Level slog.Level
		When  time.Time
		IP    *netip.Addr
		Pair  [2]int
		Extra map[string]any
	}
	ctx := t.Context()
	src := &fakeSource{data: map[string]any{"Base": map[string]any{"Count": 2}, "Level": "WARN", "When": "2026-11-11T01:02:03Z", "IP": "192.0.2.1", "Pair": []any{1, 2}, "Extra": map[string]any{"nested": []any{true}}}}
	c, err := conf.Open(ctx, Config{}, src)
	if err != nil {
		t.Fatal(err)
	}
	got := c.Current()
	if got.Count != 2 || got.Level != slog.LevelWarn || got.When.Year() != 2026 || got.IP.String() != "192.0.2.1" || got.Pair != [2]int{1, 2} || got.Extra["nested"].([]any)[0] != true {
		t.Fatal("incorrect binding")
	}
	src.data["Pair"] = []any{1}
	if err = c.Reload(ctx); err == nil || got != c.Current() {
		t.Fatal("short array accepted")
	}
	_, err = conf.Open(ctx, struct{ Text secretText }{}, &fakeSource{data: map[string]any{"Text": "CANARY"}})
	var ce *conf.Error
	if !errors.As(err, &ce) || ce.Path != "Text" || ce.Err.Error() != "invalid text value" || strings.Contains(err.Error(), "CANARY") {
		t.Fatal(err)
	}
}
func TestUnknownKeySuggestion(t *testing.T) {
	_, err := conf.Open(t.Context(), typed{}, &fakeSource{data: map[string]any{"prot": 1}})
	if err == nil || !strings.Contains(err.Error(), "did you mean port?") || strings.Contains(err.Error(), "prot") {
		t.Fatal(err)
	}
}

func TestStrictMergeTextOverlay(t *testing.T) {
	type config struct {
		Port int `conf:"port"`
	}
	src := file.FS(fstest.MapFS{"app.json": {Data: []byte(`{"port":8080}`)}}, "app.json", file.JSON)
	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	flags.Int("port", 80, "")
	if err := flags.Parse([]string{"-port=9090"}); err != nil {
		t.Fatal(err)
	}
	for _, overlay := range []conf.Source{env.NewWithLookup(map[string]string{"PORT": "port"}, func(string) (string, bool) { return "9090", true }), flagset.New(flags)} {
		cfg, err := conf.Open(t.Context(), config{}, src, overlay)
		if err != nil || cfg.Current().Port != 9090 {
			t.Fatal(err)
		}
		_, err = conf.OpenWith(t.Context(), config{}, conf.Options{Merge: conf.StrictMerge}, src, overlay)
		if cause, ok := errors.AsType[*conf.Error](err); !ok || cause.Stage != conf.StageMerge || cause.Source != 1 {
			t.Fatal(err)
		}
	}
}

type timestamp struct{ *time.Time }

func TestEmbeddedNilTextReceiver(t *testing.T) {
	src := &fakeSource{}
	cfg, err := conf.Open(t.Context(), struct{ When timestamp }{}, src)
	if err != nil {
		t.Fatal(err)
	}
	old := cfg.Current()
	src.data = map[string]any{"When": "2026-11-11T12:00:00Z"}
	err = cfg.Reload(t.Context())
	cause, ok := errors.AsType[*conf.Error](err)
	if !ok || cause.Stage != conf.StageBind || cause.Path != "When" || cfg.Current() != old {
		t.Fatalf("unexpected outcome: %v", err)
	}
}

type textPayload struct{ Value any }

func (p *textPayload) UnmarshalText(b []byte) error { p.Value = string(b); return nil }

func TestTextDecoderObjectFallback(t *testing.T) {
	src := &fakeSource{data: map[string]any{"items": []any{map[string]any{"Value": 42}, "text"}}}
	cfg, err := conf.OpenTree(t.Context(), src)
	if err != nil {
		t.Fatal(err)
	}
	values, err := conf.Get[[]textPayload](cfg.Snapshot().Tree, "items")
	if err != nil || !reflect.DeepEqual(values, []textPayload{{42}, {"text"}}) {
		t.Fatal(values, err)
	}
	_, err = conf.Open(t.Context(), struct {
		Items []textPayload `conf:"items"`
	}{}, src)
	if cause, ok := errors.AsType[*conf.Error](err); !ok || cause.Stage != conf.StageBind {
		t.Fatal(err)
	}
}

func TestPromotedTextDecoderRootPreflight(t *testing.T) {
	src := &fakeSource{}
	_, err := conf.Open(t.Context(), struct {
		time.Time
		Invalid chan int
	}{}, src)
	if cause, ok := errors.AsType[*conf.Error](err); !ok || cause.Stage != conf.StageConfig || cause.Path != "Invalid" || src.calls != 0 {
		t.Fatalf("reads=%d err=%v", src.calls, err)
	}
}

type nilPanicText string

func (v *nilPanicText) UnmarshalText(text []byte) error {
	*v = nilPanicText(text)
	panic(nil)
}

func TestNilTextPanicRollsBack(t *testing.T) {
	for _, setting := range []string{"panicnil=0", "panicnil=1"} {
		t.Run(setting, func(t *testing.T) {
			t.Setenv("GODEBUG", setting)
			type app struct{ Field nilPanicText }
			src := &fakeSource{data: map[string]any{}}
			cfg, err := conf.Open(t.Context(), app{Field: "initial"}, src)
			if err != nil {
				t.Fatal(err)
			}
			old, changes, stop := cfg.SubscribeSnapshot("Field")
			defer stop()
			src.data["Field"] = "partial"
			err = cfg.Reload(t.Context())
			if cause, ok := errors.AsType[*conf.Error](err); !ok || cause.Stage != conf.StageBind || cause.Path != "Field" {
				t.Fatalf("expected bind error at Field, got %v", err)
			}
			if cfg.Current() != old.Value || cfg.Current().Field != "initial" || cfg.Snapshot().Revision != old.Revision {
				t.Fatal("failed text decode changed the published snapshot")
			}
			select {
			case <-changes:
				t.Fatal("failed text decode notified subscriber")
			default:
			}
		})
	}
}
