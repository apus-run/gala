package conf

import (
	"errors"
	"reflect"
	"strconv"
	"testing"
)

func FuzzBind(f *testing.F) {
	for _, seed := range []string{"8080", "SECRET\nvalue", "128", "-128", "1.5"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > 4096 {
			t.Skip()
		}
		var target struct {
			Port int8 `conf:"port"`
		}
		err := bindValue(map[string]any{"port": raw}, reflect.ValueOf(&target).Elem(), fieldPath{}, false)
		if err != nil {
			ce, ok := errors.AsType[*Error](err)
			if !ok || ce.Path != "port" || ce.Stage != StageBind || errors.Unwrap(err) == nil {
				t.Fatalf("unexpected diagnostic: %v", err)
			}
			if ce.Err != errRange && ce.Err != errNotInteger && ce.Err != errTypeMismatch {
				t.Fatalf("non-fixed reason: %q", ce.Err)
			}
		}
		want, parseErr := strconv.ParseInt(raw, 10, 8)
		if (err == nil) != (parseErr == nil) || (err == nil && int64(target.Port) != want) {
			t.Fatalf("integer parse disagreement for %q", raw)
		}

		var empty struct{}
		err = bindValue(map[string]any{raw: "SECRET"}, reflect.ValueOf(&empty).Elem(), fieldPath{}, false)
		if err == nil || err.Error() != "conf: bind failed: unknown key" {
			t.Fatalf("unknown key exposed: %v", err)
		}
	})
}
