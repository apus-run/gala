package errcode

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"testing"
)

func TestImmutability(t *testing.T) {
	base := ErrNotFound
	orig := base.Message()

	got := base.WithMessage("user 7 not found").KV("id", "7")

	if base.Message() != orig || base.Metadata() != nil {
		t.Fatal("predefined error was mutated")
	}
	if got.Message() != "user 7 not found" || got.Metadata()["id"] != "7" {
		t.Fatalf("unexpected copy: %v", got)
	}
	if got.Code() != base.Code() || got.Reason() != base.Reason() {
		t.Fatal("Code/Reason must be preserved")
	}
}

func TestKV_ChainsAndDoesNotShareMap(t *testing.T) {
	a := ErrConflict.KV("k", "1")
	b := a.KV("k", "2").KV("x", "y")
	derived := a.WithMessage("m") // 与 a 共享 map，KV 必须写时复制

	if a.Metadata()["k"] != "1" || len(a.Metadata()) != 1 {
		t.Fatalf("a mutated: %v", a.Metadata())
	}
	if b.Metadata()["k"] != "2" || b.Metadata()["x"] != "y" {
		t.Fatalf("b = %v", b.Metadata())
	}
	if len(derived.Metadata()) != 1 {
		t.Fatalf("derived = %v", derived.Metadata())
	}
}

func TestMetadata_ReturnsCopy(t *testing.T) {
	e := ErrConflict.KV("tenant", "a")
	e.Metadata()["tenant"] = "b"
	if e.Metadata()["tenant"] != "a" {
		t.Fatal("Metadata() must return a copy")
	}
}

func TestIs(t *testing.T) {
	err := fmt.Errorf("load user: %w", ErrNotFound.WithMessage("user 7 not found"))
	if !errors.Is(err, ErrNotFound) {
		t.Fatal("same Code+Reason should match regardless of Message")
	}
	if errors.Is(err, ErrConflict) {
		t.Fatal("different Reason must not match")
	}
	if errors.Is(ErrNotFound, (*AppError)(nil)) {
		t.Fatal("nil *AppError target must not match (and must not panic)")
	}
	if errors.Is(New(400, ErrNotFound.Reason(), "same reason"), ErrNotFound) {
		t.Fatal("the same Reason with a different Code must not match")
	}
}

func TestWrapAndCause(t *testing.T) {
	cause := errors.New("boom")
	e := ErrInternal.Wrap(cause)
	if !errors.Is(e, cause) || errors.Unwrap(e) != cause {
		t.Fatal("cause chain broken")
	}
	if ErrInternal.Unwrap() != nil {
		t.Fatal("Wrap must not mutate the original")
	}
	other := errors.New("replacement")
	replaced := e.Wrap(other)
	if !errors.Is(replaced, other) || errors.Is(replaced, cause) || e.Unwrap() != cause {
		t.Fatal("Wrap must replace the cause on a copy without mutating the original")
	}
	if e.Wrap(nil).Unwrap() != nil {
		t.Fatal("Wrap(nil) must clear the copied cause")
	}
}

func TestError_CarriesCauseForLogsButNotForJSON(t *testing.T) {
	e := ErrInternal.Wrap(errors.New("dial postgres://user:pass@10.0.0.1:5432"))
	if !strings.Contains(e.Error(), "postgres") {
		t.Fatal("Error() should carry cause for logging")
	}
	// 字段不导出：即便有人直接 json.Marshal(err)，也不会把 cause 序列化出去.
	//nolint:staticcheck // SA9005：无导出字段正是本断言要验证的行为
	if b, _ := json.Marshal(e); string(b) != "{}" {
		t.Fatalf("json.Marshal(e) = %s", b)
	}
}

func TestLogValue(t *testing.T) {
	cause := errors.New("storage failure")
	e := ErrNotFound.Wrap(cause).KV("id", "7").WithMessage("user not found")
	err := Normalize(fmt.Errorf("load user: %w", e))
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Error("request failed", "err", err)
	var entry struct {
		Err struct {
			Code     int
			Reason   string
			Message  string
			Metadata map[string]string
			Cause    string
			Stack    []string
		}
	}
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("decoding log %s: %v", buf.String(), err)
	}
	got := entry.Err
	if got.Code != 404 || got.Reason != ErrNotFound.Reason() || got.Message != "user not found" || got.Metadata["id"] != "7" {
		t.Fatalf("unexpected log fields: %+v", got)
	}
	if !strings.Contains(got.Cause, "load user:") || !strings.Contains(got.Cause, cause.Error()) {
		t.Fatalf("unexpected log cause: %q", got.Cause)
	}
	if !strings.Contains(strings.Join(got.Stack, "\n"), "TestLogValue") {
		t.Fatalf("unexpected log stack: %v", got.Stack)
	}
	if got := (*AppError)(nil).LogValue(); got.Kind() != slog.KindString || got.String() != "<nil>" {
		t.Fatalf("typed nil logged as %v", got)
	}
}

func TestStackCapture(t *testing.T) {
	if got := ErrNotFound.Stack(); got != nil {
		t.Fatal("predefined errors must not capture an init-time stack")
	}

	e := ErrConflict.Wrap(errors.New("boom"))
	st := e.Stack()
	if !strings.Contains(strings.Join(st, "\n"), "TestStackCapture") {
		t.Fatalf("Wrap stack = %v", st)
	}
	if ErrConflict.Stack() != nil {
		t.Fatal("Wrap must not attach a stack to the shared predefined error")
	}

	// 已有栈时保留先捕获的那次，不被后续派生覆盖。
	if got := e.Wrap(errors.New("other")); !slices.Equal(got.Stack(), st) {
		t.Fatalf("Wrap overwrote the first stack: %v", got.Stack())
	}
	if got := e.WithStack(); !slices.Equal(got.Stack(), st) {
		t.Fatalf("WithStack overwrote the first stack: %v", got.Stack())
	}

	// Stack 返回副本，调用方修改不影响错误。
	st[0] = "mutated"
	if e.Stack()[0] == "mutated" {
		t.Fatal("Stack() must return a copy")
	}
}

func TestWithStackAndWrapNil(t *testing.T) {
	e := New(500, "Manual.Failure", "failed").WithStack()
	if !strings.Contains(strings.Join(e.Stack(), "\n"), "TestWithStackAndWrapNil") {
		t.Fatalf("WithStack stack = %v", e.Stack())
	}
	if got := ErrNotFound.Wrap(nil); got.Stack() != nil {
		t.Fatalf("Wrap(nil) must not capture a stack: %v", got.Stack())
	}
}
