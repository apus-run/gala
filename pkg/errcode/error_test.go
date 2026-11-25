package errcode

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
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

func TestWithMetadata_CopiesAndReplaces(t *testing.T) {
	src := map[string]string{"tenant": "a", "user": "7"}
	e := ErrConflict.WithMetadata(src)
	src["tenant"] = "b"

	if got := e.Metadata()["tenant"]; got != "a" {
		t.Fatalf("WithMetadata must copy the map, got %q", got)
	}
	if ErrConflict.Metadata() != nil {
		t.Fatal("WithMetadata must not mutate the receiver")
	}
	derived := e.WithMessage("m")
	if len(derived.Metadata()) != 2 || derived.Metadata()["user"] != "7" {
		t.Fatalf("derived metadata = %v", derived.Metadata())
	}
	if e.WithMetadata(map[string]string{"other": "1"}).Metadata()["tenant"] != "" {
		t.Fatal("WithMetadata must replace, not merge")
	}
	if e.WithMetadata(nil).Metadata() != nil {
		t.Fatal("WithMetadata(nil) must clear the metadata")
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
	if b, _ := json.Marshal(e); string(b) != "{}" { //nolint:staticcheck // 正是要验证没有可导出字段，因此不会序列化
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
	if got := (*AppError)(nil).LogValue(); got.Kind() != slog.KindString || got.String() != "<nil>" {
		t.Fatalf("typed nil logged as %v", got)
	}
}

// reasonFormat 是 README“Reason 是对外契约”一节描述的格式：点号连接的大驼峰 ASCII 片段。
var reasonFormat = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*(\.[A-Z][A-Za-z0-9]*)*$`)

// 与 README 的“预定义错误速查”一致；改动状态码或 Reason 会破坏已发布客户端，必须同时更新文档。
func TestPredefinedErrors(t *testing.T) {
	tests := []struct {
		err    *AppError
		status int
		reason string
	}{
		{ErrInvalidArgument, 400, "InvalidArgument"},
		{ErrUnauthenticated, 401, "Unauthenticated"},
		{ErrPermissionDenied, 403, "PermissionDenied"},
		{ErrNotFound, 404, "NotFound"},
		{ErrMethodNotAllowed, 405, "MethodNotAllowed"},
		{ErrConflict, 409, "Conflict"},
		{ErrTooManyRequests, 429, "TooManyRequests"},
		{ErrCanceled, 499, "Canceled"},
		{ErrInternal, 500, "InternalError"},
		{ErrNotImplemented, 501, "NotImplemented"},
		{ErrServiceUnavailable, 503, "ServiceUnavailable"},
		{ErrTimeout, 504, "DeadlineExceeded"},
	}
	seen := map[string]bool{}
	for _, tt := range tests {
		if !reasonFormat.MatchString(tt.reason) {
			t.Errorf("reason %q does not match the documented format", tt.reason)
		}
		if tt.err.Code() != tt.status || tt.err.HTTPStatus() != tt.status || tt.err.Reason() != tt.reason {
			t.Errorf("%s: got %d/%s, want %d/%s", tt.reason, tt.err.Code(), tt.err.Reason(), tt.status, tt.reason)
		}
		if tt.err.Message() == "" || seen[tt.reason] {
			t.Errorf("%s: message must be set and reason unique", tt.reason)
		}
		seen[tt.reason] = true
	}
}

func TestReasonFormat(t *testing.T) {
	for _, ok := range []string{"InvalidArgument", "NotFound.User", "Order.AlreadyPaid", "Auth.TokenExpired"} {
		if !reasonFormat.MatchString(ok) {
			t.Errorf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"", "notFound", "NOT_FOUND", "Order.", ".Order", "Order..Paid", "Order Paid", "订单.不存在"} {
		if reasonFormat.MatchString(bad) {
			t.Errorf("%q should be rejected", bad)
		}
	}
}
