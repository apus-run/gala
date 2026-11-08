package conf_test

import (
	"context"
	"errors"
	"math"
	"testing"

	conf "github.com/apus-run/gala/components/conf/v2"
)

func TestInvalidArgumentsUseConfigError(t *testing.T) {
	var nilSource *fakeSource
	for _, sources := range [][]conf.Source{nil, {nil}, {nilSource}} {
		c, err := conf.Open[typed](t.Context(), sources...)
		var ce *conf.Error
		if c != nil || !errors.As(err, &ce) || ce.Stage != conf.StageConfig {
			t.Fatalf("invalid arguments: %v, %v", c, err)
		}
	}
	if _, err := conf.Open[*typed](t.Context(), &fakeSource{}); err == nil {
		t.Fatal("pointer T accepted")
	}
}

func TestIntegerKindsCannotBypassChecks(t *testing.T) {
	for _, value := range []any{int16(300), int32(300), float32(300), float32(1.5), uint(300), uint64(300), math.NaN(), math.Inf(1)} {
		if _, err := conf.Open[typed](t.Context(), &fakeSource{data: map[string]any{"n8": value}}); err == nil {
			t.Errorf("accepted %T %v", value, value)
		}
	}
	for _, value := range []any{int8(-128), int16(127), int32(127), int64(127), float32(127), float64(127), "-128"} {
		if _, err := conf.Open[typed](t.Context(), &fakeSource{data: map[string]any{"n8": value}}); err != nil {
			t.Errorf("rejected %T: %v", value, err)
		}
	}
}

func TestNullRejectedInsideCollections(t *testing.T) {
	for _, value := range []any{nil, []any{nil}, []any{map[string]any{"hidden": nil}}, map[string]any(nil), []string(nil)} {
		_, err := conf.Open[typed](t.Context(), &fakeSource{data: map[string]any{"tags": value}})
		var ce *conf.Error
		if !errors.As(err, &ce) || ce.Stage != conf.StageMerge {
			t.Fatalf("null %T: %v", value, err)
		}
	}
}

func TestPublishedSliceDoesNotAliasSource(t *testing.T) {
	tags := []string{"old"}
	source := &fakeSource{data: map[string]any{"tags": tags}}
	c, err := conf.Open[typed](t.Context(), source)
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

func TestSourcesListIsCaptured(t *testing.T) {
	sources := []conf.Source{&fakeSource{data: map[string]any{"port": 1}}}
	c, err := conf.Open[typed](t.Context(), sources...)
	if err != nil {
		t.Fatal(err)
	}
	sources[0] = nil
	if err := c.Reload(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestUnknownFieldPath(t *testing.T) {
	_, err := conf.Open[typed](t.Context(), &fakeSource{data: map[string]any{"nested": map[string]any{"prot": "SECRET"}}})
	var ce *conf.Error
	if !errors.As(err, &ce) || ce.Path != "nested.prot" {
		t.Fatalf("path: %#v", err)
	}
}

type sourceFunc func(context.Context) (map[string]any, error)

func (f sourceFunc) Load(ctx context.Context) (map[string]any, error) { return f(ctx) }

func TestCancellationStopsFollowingSourceAndPublication(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	first := sourceFunc(func(context.Context) (map[string]any, error) { cancel(); return map[string]any{}, nil })
	next := &fakeSource{}
	c, err := conf.Open[typed](ctx, first, next)
	if c != nil || !errors.Is(err, context.Canceled) || next.calls != 0 {
		t.Fatalf("cancellation: %v, %v, calls=%d", c, err, next.calls)
	}
}

func TestSecondSourceFailurePreservesPointer(t *testing.T) {
	first := &fakeSource{data: map[string]any{"port": 1}}
	second := &fakeSource{}
	c, err := conf.Open[typed](t.Context(), first, second)
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

func BenchmarkCurrent(b *testing.B) {
	c, err := conf.Open[typed](b.Context(), &fakeSource{})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = c.Current()
	}
}

func BenchmarkReloadTwoSources(b *testing.B) {
	c, err := conf.Open[typed](b.Context(), &fakeSource{data: map[string]any{"port": 8080}}, &fakeSource{data: map[string]any{"port": "9000"}})
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
