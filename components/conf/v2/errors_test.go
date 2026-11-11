package conf_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	conf "github.com/apus-run/gala/components/conf/v2"
)

// Business validation causes are available only through Unwrap.
func TestValidateErrorIsRedacted(t *testing.T) {
	_, err := conf.Open(t.Context(), appConfig{}, &fakeSource{data: portFile(0)})
	if err == nil {
		t.Fatal("want validate error")
	}
	ce, ok := errors.AsType[*conf.Error](err)
	if !ok || ce.Stage != conf.StageValidate {
		t.Fatalf("stage != validate: %v", err)
	}
	if strings.Contains(err.Error(), "server.port") {
		t.Fatalf("Error() exposes validate text: %q", err.Error())
	}
	if errors.Unwrap(err) == nil {
		t.Fatal("Unwrap returned nil for validate error")
	}
}

func assertDiagnostic(t *testing.T, err error, stage conf.Stage, path, reason string) {
	t.Helper()
	ce, ok := errors.AsType[*conf.Error](err)
	if !ok || ce.Stage != stage || ce.Path != path || !strings.HasSuffix(err.Error(), ": "+reason) {
		t.Fatalf("diagnostic: %#v / %v", ce, err)
	}
	if strings.Contains(err.Error(), "SECRET") || strings.ContainsAny(err.Error(), "\r\n") {
		t.Fatalf("unsafe diagnostic: %v", err)
	}
}

type diagnosticSchema struct {
	Server *struct {
		Port int8 `conf:"port"`
	} `conf:"server"`
	Other struct {
		Port int8 `conf:"port"`
	} `conf:"other"`
	Labels map[string]int `conf:"labels"`
	Items  []int          `conf:"items"`
}

func TestBindSchemaPaths(t *testing.T) {
	for _, tc := range []struct {
		data         map[string]any
		path, reason string
	}{
		{map[string]any{"server": map[string]any{"port": 300}}, "server.port", "value out of range"},
		{map[string]any{"server": map[string]any{"port": "999999999999999999999999999"}}, "server.port", "value out of range"},
		{map[string]any{"other": map[string]any{"port": "SECRET"}}, "other.port", "type mismatch"},
		{map[string]any{"labels": map[string]any{"SECRET": 1.5}}, "labels", "not an exact integer"},
		{map[string]any{"items": []any{"SECRET"}}, "items", "type mismatch"},
	} {
		_, err := conf.Open(t.Context(), diagnosticSchema{}, &fakeSource{data: tc.data})
		assertDiagnostic(t, err, conf.StageBind, tc.path, tc.reason)
	}
}

func TestSchemaControlCharactersAreOmitted(t *testing.T) {
	type config struct {
		Port int `conf:"bad\nname"`
	}
	_, err := conf.Open(t.Context(), config{}, &fakeSource{data: map[string]any{"bad\nname": "SECRET"}})
	assertDiagnostic(t, err, conf.StageConfig, "Port", "invalid type for binding")
}

type validationCause struct{}

func (*validationCause) Error() string { return "SECRET\nvalidation" }

var businessCause = &validationCause{}

type validated struct {
	Port int `conf:"port"`
}

func (c *validated) Validate() error {
	if c.Port == 2 {
		return fmt.Errorf("SECRET: %w", businessCause)
	}
	return nil
}

func TestSourceAndValidationTrustBoundary(t *testing.T) {
	source := &fakeSource{data: map[string]any{"port": 1}}
	c, err := conf.Open(t.Context(), validated{}, source)
	if err != nil {
		t.Fatal(err)
	}
	old := c.Current()
	source.err = nil
	source.data["port"] = 2
	err = c.Reload(t.Context())
	assertFailure(t, err, conf.StageValidate)
	var target *validationCause
	if !errors.Is(err, businessCause) || !errors.As(err, &target) || c.Current() != old {
		t.Fatal("business cause or old snapshot lost")
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
		_, err := conf.Open(t.Context(), struct {
			Labels map[string]int `conf:"labels"`
		}{}, &fakeSource{data: data})
		assertFailure(t, err, conf.StageBind)
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
		_, err := conf.Open(t.Context(), typed{}, &fakeSource{data: tc.data})
		if err == nil || !strings.Contains(err.Error(), tc.reason) {
			t.Fatalf("want %q: %v", tc.reason, err)
		}
		if errors.Unwrap(err) == nil {
			t.Fatal("missing diagnostic cause")
		}
	}
}

func TestUnknownFieldPath(t *testing.T) {
	_, err := conf.Open(t.Context(), typed{}, &fakeSource{data: map[string]any{"nested": map[string]any{"prot": "SECRET"}}})
	ce, ok := errors.AsType[*conf.Error](err)
	if !ok || ce.Path != "nested" {
		t.Fatalf("path: %#v", err)
	}
}

func TestSourceErrorsKeepOriginalCause(t *testing.T) {
	src := &inputSource{}
	inner := &conf.Error{Stage: conf.StageMerge, Source: 77, Path: "other", Err: errors.New("source error")}
	src.err = inner
	cfg, err := conf.Open(t.Context(), typed{}, src)
	var outer *conf.Error
	if cfg != nil || !errors.As(err, &outer) || outer.Stage != conf.StageLoad || outer.Source != 0 || outer.Path != "" || !errors.Is(err, inner) {
		t.Fatalf("wrong load error: %v", err)
	}
}
