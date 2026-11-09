package conf

import (
	"errors"
	"testing"
)

func FuzzBindDiagnostics(f *testing.F) {
	for _, seed := range []string{"8080", "SECRET\nvalue", "128", "-128", "1.5"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > 4096 {
			t.Skip()
		}
		var target struct {
			Port int8 `mapstructure:"port"`
		}
		err := bind(map[string]any{"port": raw}, &target)
		if err != nil {
			ce, ok := errors.AsType[*Error](err)
			if !ok || ce.Path != "port" || ce.Stage != StageBind || errors.Unwrap(err) != nil {
				t.Fatalf("unexpected diagnostic: %v", err)
			}
			if ce.reason != errRange.Error() && ce.reason != errNotInteger.Error() {
				t.Fatalf("non-fixed reason: %q", ce.reason)
			}
		}
		var empty struct{}
		err = bind(map[string]any{raw: "SECRET"}, &empty)
		if err == nil || err.Error() != "conf: bind failed" {
			t.Fatalf("unknown key exposed: %v", err)
		}
	})
}
