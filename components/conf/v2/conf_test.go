package conf_test

import (
	"context"
	"errors"
	"maps"
	"strings"
	"sync"
	"testing"

	conf "github.com/apus-run/gala/components/conf/v2"
)

// fakeSource returns a fixed map, or an error, and counts Load calls.
// fakeSource 返回固定 map 或错误，并统计 Load 次数。
type fakeSource struct {
	mu    sync.Mutex
	data  map[string]any
	err   error
	calls int
}

func (f *fakeSource) Load(context.Context) (map[string]any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	out := map[string]any{}
	maps.Copy(out, f.data)
	return out, nil
}

type appConfig struct {
	Server struct {
		Port int `conf:"port"`
	} `conf:"server"`
}

func (c *appConfig) Validate() error {
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return errors.New("server.port: must be in 1..65535")
	}
	return nil
}

func portFile(port any) map[string]any {
	return map[string]any{"server": map[string]any{"port": port}}
}

func portEnv(port string) map[string]any {
	return map[string]any{"server": map[string]any{"port": port}}
}

// C04: a first-load failure yields no instance and no panic.
// C04：首次加载失败时不返回实例，也不 panic。
func TestC04FirstLoadFailureReturnsNil(t *testing.T) {
	file := &fakeSource{err: errors.New("disk on fire /secret/path/cfg.yaml")}
	c, err := conf.Open(t.Context(), appConfig{}, file)
	if err == nil {
		t.Fatal("open: want error")
	}
	if c != nil {
		t.Fatal("open: want nil config on failure")
	}
}

func TestZeroValueConfigDoesNotBlock(t *testing.T) {
	var c conf.Config[typed]
	if c.Current() != nil {
		t.Fatal("non-nil zero value")
	}
	assertFailure(t, c.Reload(t.Context()), conf.StageConfig)
}

func TestInvalidArgumentsStateReason(t *testing.T) {
	_, err := conf.Open(t.Context(), typed{})
	if err == nil || !strings.Contains(err.Error(), "at least one source") {
		t.Fatalf("reason: %v", err)
	}
	_, err = conf.Open(t.Context(), typed{}, nil)
	if err == nil || !strings.Contains(err.Error(), "source must not be nil") {
		t.Fatalf("reason: %v", err)
	}
	_, err = conf.Open(t.Context(), (*typed)(nil), &fakeSource{})
	if err == nil || !strings.Contains(err.Error(), "struct value type") {
		t.Fatalf("reason: %v", err)
	}
}

func TestInvalidArgumentsUseConfigError(t *testing.T) {
	var nilSource *fakeSource
	for _, sources := range [][]conf.Source{nil, {nil}, {nilSource}} {
		c, err := conf.Open(t.Context(), typed{}, sources...)
		ce, ok := errors.AsType[*conf.Error](err)
		if c != nil || !ok || ce.Stage != conf.StageConfig {
			t.Fatalf("invalid arguments: %v, %v", c, err)
		}
	}
	if _, err := conf.Open(t.Context(), (*typed)(nil), &fakeSource{}); err == nil {
		t.Fatal("pointer T accepted")
	}
}

func TestSourcesListIsCaptured(t *testing.T) {
	sources := []conf.Source{&fakeSource{data: map[string]any{"port": 1}}}
	c, err := conf.Open(t.Context(), typed{}, sources...)
	if err != nil {
		t.Fatal(err)
	}
	sources[0] = nil
	if err := c.Reload(t.Context()); err != nil {
		t.Fatal(err)
	}
}

type inputSource struct {
	data  map[string]any
	err   error
	reads int
}

func (s *inputSource) Load(context.Context) (map[string]any, error) { s.reads++; return s.data, s.err }

func (*inputSource) String() string { panic("source.String called") }

func BenchmarkCurrent(b *testing.B) {
	c, err := conf.Open(b.Context(), typed{}, &fakeSource{})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = c.Current()
	}
}

func BenchmarkReloadTwoSources(b *testing.B) {
	c, err := conf.Open(b.Context(), typed{}, &fakeSource{data: map[string]any{"port": 8080}}, &fakeSource{data: map[string]any{"port": "9000"}})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if err := c.Reload(b.Context()); err != nil {
			b.Fatal(err)
		}
	}
}
