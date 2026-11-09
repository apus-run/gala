package conf_test

import (
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	conf "github.com/apus-run/gala/components/conf/v2"
	"github.com/apus-run/gala/components/conf/v2/sources/file"
	_ "github.com/apus-run/gala/pkg/encoding/json"
)

func TestDocumentFailureReasons(t *testing.T) {
	for _, tc := range []struct{ name, body, reason string }{
		{"config.notregistered", "SECRET", "codec not registered"},
		{"empty.json", " \n", "document is empty"},
		{"broken.json", `{"SECRET":`, "document could not be decoded"},
		{"array.json", `["SECRET"]`, "document root must be an object"},
		{"null.json", `null`, "document root must be an object"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := fstest.MapFS{tc.name: &fstest.MapFile{Data: []byte(`{"port":1}`)}}
			// A valid lower layer makes a failed reload's publication observable.
			src := file.NewFS(data, tc.name)
			if tc.name == "config.notregistered" {
				_, err := conf.Open[typed](t.Context(), conf.WithSources(src))
				assertDiagnostic(t, err, conf.StageDecode, "", tc.reason)
				return
			}
			cfg, err := conf.Open[typed](t.Context(), conf.WithSources(src))
			if err != nil {
				t.Fatal(err)
			}
			old := cfg.Current()
			data[tc.name].Data = []byte(tc.body)
			err = cfg.Reload(t.Context())
			assertDiagnostic(t, err, conf.StageDecode, "", tc.reason)
			if cfg.Current() != old {
				t.Fatal("failed decode published")
			}
		})
	}
}

func assertDiagnostic(t *testing.T, err error, stage conf.Stage, path, reason string) {
	t.Helper()
	ce, ok := errors.AsType[*conf.Error](err)
	if !ok || ce.Stage != stage || ce.Path != path || !strings.HasSuffix(err.Error(), ": "+reason) {
		t.Fatalf("diagnostic: %#v / %v", ce, err)
	}
	if errors.Unwrap(err) != nil || strings.Contains(err.Error(), "SECRET") || strings.ContainsAny(err.Error(), "\r\n") {
		t.Fatalf("unsafe diagnostic: %v", err)
	}
}

type diagnosticSchema struct {
	Server *struct {
		Port int8 `mapstructure:"port"`
	} `mapstructure:"server"`
	Other struct {
		Port int8 `mapstructure:"port"`
	} `mapstructure:"other"`
	Labels map[string]int `mapstructure:"labels"`
	Items  []int          `mapstructure:"items"`
}

func TestBindSchemaPaths(t *testing.T) {
	for _, tc := range []struct {
		data         map[string]any
		path, reason string
	}{
		{map[string]any{"server": map[string]any{"port": 300}}, "server.port", "value out of range"},
		{map[string]any{"server": map[string]any{"port": "999999999999999999999999999"}}, "server.port", "value out of range"},
		{map[string]any{"other": map[string]any{"port": "SECRET"}}, "other.port", "not an exact integer"},
		{map[string]any{"labels": map[string]any{"SECRET": 1.5}}, "", "not an exact integer"},
		{map[string]any{"items": []any{"SECRET"}}, "", "not an exact integer"},
	} {
		_, err := conf.Open[diagnosticSchema](t.Context(), conf.WithSources(&fakeSource{data: tc.data}))
		assertDiagnostic(t, err, conf.StageBind, tc.path, tc.reason)
	}
}

func TestUntrustedDecodeReasonIsNotCopied(t *testing.T) {
	for _, cause := range []error{errors.New("SECRET codec not registered"), errors.New("codec not registered")} {
		src := &fakeSource{err: conf.SourceError(conf.StageDecode, "SECRET", "SECRET", cause)}
		_, err := conf.Open[typed](t.Context(), conf.WithSources(src))
		ce, ok := errors.AsType[*conf.Error](err)
		if !ok || ce.Stage != conf.StageDecode || ce.Path != "" || ce.Name != "" || err.Error() != "conf: decode failed (source 0)" || errors.Unwrap(err) != nil {
			t.Fatalf("untrusted error: %v", err)
		}
	}
}

func TestSchemaControlCharactersAreOmitted(t *testing.T) {
	type config struct {
		Port int `mapstructure:"bad\nname"`
	}
	_, err := conf.Open[config](t.Context(), conf.WithSources(&fakeSource{data: map[string]any{"bad\nname": "SECRET"}}))
	assertDiagnostic(t, err, conf.StageBind, "", "not an exact integer")
}
