package conf_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	conf "github.com/apus-run/gala/components/conf/v2"
	"github.com/apus-run/gala/components/conf/v2/sources/env"
)

// C01: a higher-priority env layer keeps winning when the file changes.
// C01：文件变化时，靠后的环境变量层仍然胜出。
func TestC01EnvWinsAfterFileReload(t *testing.T) {
	file := &fakeSource{data: portFile(8080)}
	env := &fakeSource{data: portEnv("9000")}

	cfg, err := conf.Open(t.Context(), appConfig{}, file, env)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if got := cfg.Current().Server.Port; got != 9000 {
		t.Fatalf("initial port = %d, want 9000", got)
	}

	file.data = portFile(8081)
	if err := cfg.Reload(t.Context()); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := cfg.Current().Server.Port; got != 9000 {
		t.Fatalf("after file change port = %d, want 9000 (env still wins)", got)
	}
}

// C02: removing the override restores the file value; a failed reload keeps the
// previous published value.
// C02：移除覆盖后恢复文件值；重载失败时保留旧的已发布值。
func TestC02RemoveOverrideAndKeepLastGood(t *testing.T) {
	file := &fakeSource{data: portFile(8081)}
	env := &fakeSource{data: portEnv("9000")}

	cfg, err := conf.Open(t.Context(), appConfig{}, file, env)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	env.data = map[string]any{}
	if err := cfg.Reload(t.Context()); err != nil {
		t.Fatalf("reload without env: %v", err)
	}
	if got := cfg.Current().Server.Port; got != 8081 {
		t.Fatalf("port after env removed = %d, want 8081", got)
	}

	file.data = portFile(0)
	if err := cfg.Reload(t.Context()); err == nil {
		t.Fatal("reload with invalid port: want error")
	}
	if got := cfg.Current().Server.Port; got != 8081 {
		t.Fatalf("port after failed validate = %d, want 8081 (kept)", got)
	}
}

// C03: a bind error on one field leaves every field unchanged, and the same
// instance recovers once the input is fixed.
// C03：单个字段绑定失败时所有字段都保持不变；修正输入后同一实例可恢复。
func TestC03BindErrorIsAtomic(t *testing.T) {
	type cfg struct {
		Name string `conf:"name"`
		Port int    `conf:"port"`
	}
	file := &fakeSource{data: map[string]any{"name": "old", "port": 8080}}
	c, err := conf.Open(t.Context(), cfg{}, file)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	file.data = map[string]any{"name": "new", "port": "abc"}
	if err := c.Reload(t.Context()); err == nil {
		t.Fatal("reload with bad port: want error")
	}
	if got := c.Current().Name; got != "old" {
		t.Fatalf("name = %q after failed batch, want %q", got, "old")
	}

	file.data = map[string]any{"name": "new", "port": 9090}
	if err := c.Reload(t.Context()); err != nil {
		t.Fatalf("reload after fix: %v", err)
	}
	if got := c.Current(); got.Name != "new" || got.Port != 9090 {
		t.Fatalf("after fix = %+v, want name=new port=9090", *got)
	}
}

func TestReloadFailureWrapsSourceAndKeepsOld(t *testing.T) {
	file := &fakeSource{data: portFile(8080)}
	c, err := conf.Open(t.Context(), appConfig{}, file)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	file.err = errors.New("source unavailable")

	err = c.Reload(t.Context())
	if err == nil {
		t.Fatal("reload: want error")
	}
	if got := c.Current().Server.Port; got != 8080 {
		t.Fatalf("port = %d, want 8080 kept", got)
	}
	if !errors.Is(err, file.err) || !strings.Contains(err.Error(), file.err.Error()) {
		t.Fatalf("missing source cause: %v", err)
	}
}

func TestEnvMappingSnapshotAndValueRefresh(t *testing.T) {
	for _, process := range []bool{false, true} {
		t.Run(fmt.Sprint(process), func(t *testing.T) {
			bindings := map[string]string{"CONF_ACCEPT_PORT": "port"}
			value := "8080"
			var source conf.Source
			if process {
				t.Setenv("CONF_ACCEPT_PORT", value)
				source = env.New(bindings)
			} else {
				source = env.NewWithLookup(bindings, func(string) (string, bool) { return value, true })
			}
			bindings["CONF_ACCEPT_PORT"] = "SECRET"
			c, err := conf.Open(t.Context(), typed{}, source)
			if err != nil || c.Current().Port != 8080 {
				t.Fatalf("mapping not captured: %v", err)
			}
			value = "9090"
			if process {
				t.Setenv("CONF_ACCEPT_PORT", value)
			}
			if err := c.Reload(t.Context()); err != nil || c.Current().Port != 9090 {
				t.Fatalf("stale value: %v", err)
			}
		})
	}
}

func TestInvalidEnvMappingDoesNotLookup(t *testing.T) {
	calls := 0
	source := env.NewWithLookup(map[string]string{"A": "port", "B": "port"}, func(string) (string, bool) { calls++; return "1", true })
	_, err := conf.Open(t.Context(), typed{}, source)
	assertFailure(t, err, conf.StageLoad)
	if calls != 0 {
		t.Fatal("lookup before validation")
	}
}

func TestEmptySourcesLeaveDefaults(t *testing.T) {
	for _, layer := range []map[string]any{nil, {}} {
		c, err := conf.Open(t.Context(), typed{}, &fakeSource{data: map[string]any{"port": 8080}}, &fakeSource{data: layer})
		if err != nil || c.Current().Port != 8080 {
			t.Fatalf("empty layer: %v", err)
		}
	}
}

func TestRejectedSourceValuesPreserveSnapshot(t *testing.T) {
	type nested struct {
		Values []string `conf:"values"`
	}
	type app struct {
		Nested nested `conf:"nested"`
	}
	cycle := map[string]any{}
	cycle["self"] = cycle
	for _, value := range []any{nested{Values: []string{"SECRET"}}, cycle} {
		source := &fakeSource{data: map[string]any{"nested": map[string]any{"values": []string{"old"}}}}
		c, err := conf.Open(t.Context(), app{}, source)
		if err != nil {
			t.Fatal(err)
		}
		old := c.Current()
		source.data["nested"] = value
		err = c.Reload(t.Context())
		assertFailure(t, err, conf.StageMerge)
		if errors.Unwrap(err) == nil || c.Current() != old || old.Nested.Values[0] != "old" {
			t.Fatal("failed reload changed snapshot or exposed source")
		}
		source.data["nested"] = map[string]any{"values": []string{"new"}}
		if err := c.Reload(t.Context()); err != nil {
			t.Fatal(err)
		}
		if c.Current().Nested.Values[0] != "new" || old.Nested.Values[0] != "old" {
			t.Fatal("recovery broke snapshot isolation")
		}
	}
}

func TestReloadRemovesAbsentKeys(t *testing.T) {
	data := map[string]any{"port": 1}
	source := conf.SourceFunc(func(context.Context) (map[string]any, error) { return data, nil })
	c, err := conf.Open(t.Context(), typed{}, source)
	if err != nil {
		t.Fatal(err)
	}

	old := c.Current()
	data = map[string]any{}
	if err := c.Reload(t.Context()); err != nil {
		t.Fatal(err)
	}
	if c.Current().Port != 0 || old.Port != 1 {
		t.Fatal("removed key survived a full rebuild or old snapshot changed")
	}
}

// Every accepted object representation must have the same merge semantics.
func TestTypedObjectsMergeRecursively(t *testing.T) {
	type key string
	type object map[key]int
	type app struct {
		Limits struct {
			Read  int `conf:"read"`
			Write int `conf:"write"`
		} `conf:"limits"`
	}
	for name, override := range map[string]any{
		"ordinary": map[string]any{"read": 3},
		"typed":    map[string]int{"read": 3},
		"named":    object{"read": 3},
	} {
		t.Run(name, func(t *testing.T) {
			base := &fakeSource{data: map[string]any{"limits": object{"read": 1, "write": 2}}}
			high := &fakeSource{data: map[string]any{"limits": override}}
			c, err := conf.Open(t.Context(), app{}, base, high)
			if err != nil {
				t.Fatal(err)
			}

			if got := c.Current().Limits; got.Read != 3 || got.Write != 2 {
				t.Fatalf("object merge lost lower-layer fields: %+v", got)
			}
			if base.data["limits"].(object)["read"] != 1 {
				t.Fatal("merge modified source data")
			}
		})
	}
}

func TestNullRejectedInsideCollections(t *testing.T) {
	for _, value := range []any{nil, []any{nil}, []any{map[string]any{"hidden": nil}}, map[string]any(nil), []string(nil)} {
		_, err := conf.Open(t.Context(), typed{}, &fakeSource{data: map[string]any{"tags": value}})
		ce, ok := errors.AsType[*conf.Error](err)
		if !ok || ce.Stage != conf.StageMerge {
			t.Fatalf("null %T: %v", value, err)
		}
	}
}

func TestPublishedSliceDoesNotAliasSource(t *testing.T) {
	tags := []string{"old"}
	source := &fakeSource{data: map[string]any{"tags": tags}}
	c, err := conf.Open(t.Context(), typed{}, source)
	if err != nil {
		t.Fatal(err)
	}
	old := c.Current()
	tags[0] = "new"
	if old.Tags[0] != "old" {
		t.Fatal("source mutated published slice")
	}
	if err := c.Reload(t.Context()); err != nil {
		t.Fatal(err)
	}
	if old.Tags[0] != "old" || c.Current().Tags[0] != "new" {
		t.Fatal("snapshots not independent")
	}
}

func TestSecondSourceFailurePreservesPointer(t *testing.T) {
	first := &fakeSource{data: map[string]any{"port": 1}}
	second := &fakeSource{}
	c, err := conf.Open(t.Context(), typed{}, first, second)
	if err != nil {
		t.Fatal(err)
	}
	old := c.Current()
	first.data["port"] = 2
	second.err = errors.New("secret")
	if err := c.Reload(t.Context()); err == nil || c.Current() != old || old.Port != 1 {
		t.Fatal("failed batch published")
	}
	second.err = nil
	if err := c.Reload(t.Context()); err != nil || c.Current().Port != 2 {
		t.Fatalf("recovery: %v", err)
	}
}

type defaultsConfig struct {
	Number  int
	Enabled bool
	Text    string
	Values  []string
	Map     map[string]int
	Ptr     *struct{ A, B int }
	Nested  struct{ A, B int }
}

func (c *defaultsConfig) Validate() error {
	if c.Number < 0 {
		return errors.New("SECRET\ninvalid")
	}
	return nil
}

func TestPrefilledSnapshots(t *testing.T) {
	def := defaultsConfig{Number: 5, Enabled: true, Text: "default", Values: []string{"old"}, Map: map[string]int{"old": 1}, Ptr: &struct{ A, B int }{3, 4}, Nested: struct{ A, B int }{3, 4}}
	src := &inputSource{}
	c, err := conf.Open(t.Context(), def, src)
	if err != nil {
		t.Fatal(err)
	}
	old := c.Current()
	src.data = map[string]any{"Number": -1, "Enabled": false, "Text": "", "Values": []string{}, "Map": map[string]int{}, "Ptr": map[string]int{"A": 9}, "Nested": map[string]int{"A": 9}}
	assertFailure(t, c.Reload(t.Context()), conf.StageValidate)
	if c.Current() != old || def.Map["old"] != 1 || def.Ptr.A != 3 || def.Values[0] != "old" {
		t.Fatal("candidate mutated defaults")
	}
	src.data["Number"] = 0
	if err := c.Reload(t.Context()); err != nil {
		t.Fatal(err)
	}
	next := c.Current()
	if next.Number != 0 || next.Enabled || next.Text != "" || next.Values == nil || len(next.Values) != 0 || next.Map == nil || len(next.Map) != 0 || next.Ptr == def.Ptr || next.Ptr.B != 0 || next.Nested.B != 4 {
		t.Fatalf("defaults: %+v", next)
	}
	src.data = nil
	if err := c.Reload(t.Context()); err != nil || !reflect.DeepEqual(*c.Current(), def) {
		t.Fatalf("missing keys: %v", err)
	}
}

func TestExpandedLimitRollbackAcrossSources(t *testing.T) {
	a, b, last := &inputSource{}, &inputSource{}, &inputSource{}
	type config struct{ Items []string }
	c, err := conf.Open(t.Context(), config{}, a, b, last)
	if err != nil {
		t.Fatal(err)
	}
	old := c.Current()
	a.data = map[string]any{"Items": make([]string, 32766)}
	b.data = map[string]any{"Items": make([]string, 32767)}
	err = c.Reload(t.Context())
	var ce *conf.Error
	if !errors.As(err, &ce) || ce.Stage != conf.StageMerge || ce.Source != 1 || !strings.Contains(err.Error(), "expanded node limit exceeded") || last.reads != 1 || c.Current() != old {
		t.Fatalf("over budget: %v", err)
	}
	b.data = nil
	if err := c.Reload(t.Context()); err != nil {
		t.Fatal(err)
	}
}
