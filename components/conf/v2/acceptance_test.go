package conf_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	conf "github.com/apus-run/gala/components/conf/v2"
	"github.com/apus-run/gala/components/conf/v2/sources/env"
)

type validationCause struct{}

func (*validationCause) Error() string { return "SECRET\nvalidation" }

var businessCause = &validationCause{}

type validated struct {
	Port int `mapstructure:"port"`
}

func (c *validated) Validate() error {
	if c.Port == 2 {
		return fmt.Errorf("SECRET: %w", businessCause)
	}
	return nil
}

func TestSourceAndValidationTrustBoundary(t *testing.T) {
	for _, stage := range []conf.Stage{conf.StageValidate, "SECRET\nunknown", conf.StageMerge, conf.StageBind, conf.StageCanceled} {
		t.Run(string(stage), func(t *testing.T) {
			source := &fakeSource{data: map[string]any{"port": 1}}
			c, err := conf.Open[validated](t.Context(), conf.WithSources(source))
			if err != nil {
				t.Fatal(err)
			}
			old := c.Current()
			// A source may construct Error directly, not just use SourceError.
			source.err = &conf.Error{Stage: stage, Name: "SECRET\nname", Path: "SECRET\npath"}
			err = c.Reload(t.Context())
			assertFailure(t, err, conf.StageRead)
			if errors.Is(err, businessCause) || c.Current() != old {
				t.Fatal("source crossed trust boundary")
			}
			source.err = conf.SourceError(stage, "SECRET", "SECRET", businessCause)
			err = c.Reload(t.Context())
			assertFailure(t, err, conf.StageRead)
			if errors.Is(err, businessCause) {
				t.Fatal("untrusted error chain exposed")
			}
			source.err = nil
			source.data["port"] = 2
			err = c.Reload(t.Context())
			assertFailure(t, err, conf.StageValidate)
			var target *validationCause
			if !errors.Is(err, businessCause) || !errors.As(err, &target) || c.Current() != old {
				t.Fatal("business cause or old snapshot lost")
			}
		})
	}
}

func assertFailure(t *testing.T, err error, stage conf.Stage) {
	t.Helper()
	ce, ok := errors.AsType[*conf.Error](err)
	if !ok || ce.Stage != stage {
		t.Fatalf("want %s: %v", stage, err)
	}
	if strings.ContainsAny(err.Error(), "\n\r") || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("unsafe error %q", err)
	}
}

func TestUntrustedKeysAreNotDiagnosticPaths(t *testing.T) {
	for _, data := range []map[string]any{
		{"SECRET\nkey": 1},
		{"labels": map[string]any{"SECRET\nkey": "bad"}},
	} {
		_, err := conf.Open[struct {
			Labels map[string]int `mapstructure:"labels"`
		}](t.Context(), conf.WithSources(&fakeSource{data: data}))
		assertFailure(t, err, conf.StageBind)
	}
}

func TestCanceledLoadWinsOverReturnedError(t *testing.T) {
	for _, sentinel := range []error{context.Canceled, context.DeadlineExceeded} {
		source := &fakeSource{data: map[string]any{"port": 1}}
		next := &fakeSource{}
		c, err := conf.Open[typed](t.Context(), conf.WithSources(source, next))
		if err != nil {
			t.Fatal(err)
		}
		old, calls := c.Current(), next.calls
		source.err = &conf.DecodeError{Err: fmt.Errorf("SECRET: %w", sentinel)}
		err = c.Reload(t.Context())
		assertFailure(t, err, conf.StageCanceled)
		if !errors.Is(err, sentinel) || c.Current() != old || next.calls != calls {
			t.Fatal("cancellation contract")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	source := sourceFunc(func(context.Context) (map[string]any, error) {
		cancel()
		return nil, &conf.DecodeError{Err: errors.New("SECRET")}
	})
	_, err := conf.Open[typed](ctx, conf.WithSources(source))
	assertFailure(t, err, conf.StageCanceled)
	if !errors.Is(err, context.Canceled) {
		t.Fatal("ctx did not take precedence")
	}
}

func TestZeroValueConfigDoesNotBlock(t *testing.T) {
	var c conf.Config[typed]
	if c.Current() != nil {
		t.Fatal("non-nil zero value")
	}
	assertFailure(t, c.Reload(t.Context()), conf.StageConfig)
}

func TestAbsentUnsupportedFieldThenBindFails(t *testing.T) {
	source := &fakeSource{data: map[string]any{}}
	c, err := conf.Open[struct {
		Rate float64 `mapstructure:"rate"`
	}](t.Context(), conf.WithSources(source))
	if err != nil {
		t.Fatal(err)
	}
	old := c.Current()
	source.data["rate"] = 1.5
	assertFailure(t, c.Reload(t.Context()), conf.StageBind)
	if old != c.Current() || old.Rate != 0 {
		t.Fatal("failed batch published")
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
			c, err := conf.Open[typed](t.Context(), conf.WithSources(source))
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

func TestOldNestedSnapshotDuringReload(t *testing.T) {
	type config struct {
		Entries map[string][]int `mapstructure:"entries"`
	}
	entries := map[string][]int{"entry": {1, 2}}
	source := &fakeSource{data: map[string]any{"entries": entries}}
	c, err := conf.Open[config](t.Context(), conf.WithSources(source))
	if err != nil {
		t.Fatal(err)
	}
	old := c.Current()
	var wg sync.WaitGroup
	start := make(chan struct{})
	wg.Go(func() {
		<-start
		for range 1000 {
			if old.Entries["entry"][0] != 1 || old.Entries["entry"][1] != 2 {
				t.Error("old snapshot changed")
				return
			}
		}
	})
	close(start)
	entries["entry"][0] = 3
	entries["entry"] = []int{4, 5}
	if err := c.Reload(t.Context()); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	if c.Current().Entries["entry"][0] != 4 || old.Entries["entry"][0] != 1 {
		t.Fatal("snapshots alias")
	}
}

func TestInvalidEnvMappingDoesNotLookup(t *testing.T) {
	calls := 0
	source := env.NewWithLookup(map[string]string{"A": "port", "B": "port"}, func(string) (string, bool) { calls++; return "1", true })
	_, err := conf.Open[typed](t.Context(), conf.WithSources(source))
	assertFailure(t, err, conf.StageConfig)
	if calls != 0 {
		t.Fatal("lookup before validation")
	}
}

func TestEmptySourcesLeaveDefaults(t *testing.T) {
	for _, layer := range []map[string]any{nil, {}} {
		c, err := conf.Open[typed](t.Context(), conf.WithSources(&fakeSource{data: map[string]any{"port": 8080}}, &fakeSource{data: layer}))
		if err != nil || c.Current().Port != 8080 {
			t.Fatalf("empty layer: %v", err)
		}
	}
}

type namedFailure struct{}

func (namedFailure) Error() string    { return "SECRET\nerror" }
func (namedFailure) SafeName() string { return "SECRET\nname" }

func TestSourceCannotForwardBusinessErrorOrSafeName(t *testing.T) {
	_, validationErr := conf.Open[validated](t.Context(), conf.WithSources(&fakeSource{data: map[string]any{"port": 2}}))
	for _, cause := range []error{validationErr, namedFailure{}} {
		_, err := conf.Open[typed](t.Context(), conf.WithSources(&fakeSource{err: cause}))
		assertFailure(t, err, conf.StageRead)
		if errors.Is(err, businessCause) {
			t.Fatal("forwarded business cause")
		}
	}
}

func TestInvalidArgumentsStateReason(t *testing.T) {
	_, err := conf.Open[typed](t.Context(), conf.WithSources())
	if err == nil || !strings.Contains(err.Error(), "at least one source") {
		t.Fatalf("reason: %v", err)
	}
	_, err = conf.Open[typed](t.Context(), conf.WithSources(nil))
	if err == nil || !strings.Contains(err.Error(), "source must not be nil") {
		t.Fatalf("reason: %v", err)
	}
	_, err = conf.Open[*typed](t.Context(), conf.WithSources(&fakeSource{}))
	if err == nil || !strings.Contains(err.Error(), "struct value type") {
		t.Fatalf("reason: %v", err)
	}
}

func TestRejectedSourceValuesPreserveSnapshot(t *testing.T) {
	type nested struct {
		Values []string `mapstructure:"values"`
	}
	type app struct {
		Nested nested `mapstructure:"nested"`
	}
	cycle := map[string]any{}
	cycle["self"] = cycle
	for _, value := range []any{nested{Values: []string{"SECRET"}}, cycle} {
		source := &fakeSource{data: map[string]any{"nested": map[string]any{"values": []string{"old"}}}}
		c, err := conf.Open[app](t.Context(), conf.WithSources(source))
		if err != nil {
			t.Fatal(err)
		}
		old := c.Current()
		source.data["nested"] = value
		err = c.Reload(t.Context())
		assertFailure(t, err, conf.StageMerge)
		if errors.Unwrap(err) != nil || c.Current() != old || old.Nested.Values[0] != "old" {
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

func TestFixedMergeAndBindDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		data   map[string]any
		reason string
	}{
		{map[string]any{"name": nil}, "explicit null"},
		{map[string]any{"name": struct{}{}}, "unsupported source value"},
		{map[string]any{"timeout": "500"}, "explicit unit"},
		{map[string]any{"n8": 300}, "out of range"},
		{map[string]any{"port": 1.5}, "exact integer"},
	} {
		_, err := conf.Open[typed](t.Context(), conf.WithSources(&fakeSource{data: tc.data}))
		if err == nil || !strings.Contains(err.Error(), tc.reason) {
			t.Fatalf("want %q: %v", tc.reason, err)
		}
		if errors.Unwrap(err) != nil {
			t.Fatal("diagnostic leaked cause")
		}
	}
}
