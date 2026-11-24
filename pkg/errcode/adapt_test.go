package errcode

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAppHandler_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("DELETE /users/7", AppHandler(func(w http.ResponseWriter, _ *http.Request) *AppError {
		w.WriteHeader(http.StatusNoContent)
		return nil
	}))
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, httptest.NewRequest(http.MethodDelete, "/users/7", http.NoBody))
	if res.Code != http.StatusNoContent || res.Body.Len() != 0 || res.Header().Get("Content-Type") != "" {
		t.Fatalf("status = %d, headers = %v, body = %q", res.Code, res.Header(), res.Body.String())
	}
}

func TestAppHandler_Error(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.Default()
	t.Cleanup(func() { slog.SetDefault(logger) })
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))

	const privateCause = "storage lookup failed"
	h := AppHandler(func(_ http.ResponseWriter, _ *http.Request) *AppError {
		return ErrNotFound.Wrap(errors.New(privateCause))
	})
	res := httptest.NewRecorder()
	h.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/users/7", http.NoBody))
	if res.Code != http.StatusNotFound || res.Header().Get("Content-Type") != ContentType {
		t.Fatalf("status = %d, headers = %v", res.Code, res.Header())
	}
	var body Body
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Status != res.Code || body.Reason != ErrNotFound.Reason() || body.Detail != ErrNotFound.Message() {
		t.Fatalf("body = %+v", body)
	}
	var entry struct {
		Err struct {
			Code   int
			Reason string
			Cause  string
		}
	}
	if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	if entry.Err.Code != res.Code || entry.Err.Reason != body.Reason || entry.Err.Cause != privateCause {
		t.Fatalf("unexpected error log: %s", logs.String())
	}
	if strings.Contains(res.Body.String(), privateCause) {
		t.Fatalf("response exposed cause: %s", res.Body.String())
	}
}

func TestAppHandler_HeadWithInvalidStatus(t *testing.T) {
	h := AppHandler(func(w http.ResponseWriter, _ *http.Request) *AppError {
		w.Header().Set("Content-Length", "42")
		return &AppError{}
	})
	res := httptest.NewRecorder()
	h.ServeHTTP(res, httptest.NewRequest(http.MethodHead, "/", http.NoBody))
	if res.Code != http.StatusInternalServerError || res.Body.Len() != 0 ||
		res.Header().Get("Content-Type") != ContentType || res.Header().Get("Content-Length") != "" {
		t.Fatalf("status = %d, headers = %v, body = %q", res.Code, res.Header(), res.Body.String())
	}
}

func TestHTTPStatus(t *testing.T) {
	var success *AppError
	if got := success.HTTPStatus(); got != http.StatusOK {
		t.Fatalf("nil error: HTTPStatus() = %d", got)
	}
	invalid := New(http.StatusOK, "Invalid", "invalid status")
	if invalid.HTTPStatus() != http.StatusInternalServerError || invalid.Code() != http.StatusOK {
		t.Fatalf("HTTPStatus() = %d, Code() = %d", invalid.HTTPStatus(), invalid.Code())
	}
}

func TestRespond(t *testing.T) {
	code, b := Respond(ErrNotFound.WithMessage("user 7 not found").KV("id", "7"))
	if code != 404 || b.Status != 404 || b.Title != "Not Found" || b.Reason != "NotFound" || b.Detail != "user 7 not found" || b.Metadata["id"] != "7" {
		t.Fatalf("got %d %+v", code, b)
	}

	if code, b = Respond(nil); code != 200 || b.Reason != "" || b.Detail != "" {
		t.Fatalf("nil => %d %+v", code, b)
	}
	// 最自然的组合：先归一化（日志要用），再生成响应。成功请求不能被当成失败。
	if code, b = Respond(Normalize(nil)); code != 200 || b.Reason != "" || b.Detail != "" {
		t.Fatalf("Respond(Normalize(nil)) => %d %+v", code, b)
	}

}

func TestRespond_StatusRange(t *testing.T) {
	var code int
	var b Body
	// 不符合本包错误响应约定的状态回退为 500，包括 1xx、2xx 和 3xx。
	for _, bad := range []int{0, 99, 100, 199, 200, 201, 204, 205, 302, 304, 399, 600, 999, 1000} {
		if code, b = Respond(New(bad, "Bad", "x")); code != 500 || b.Status != 500 {
			t.Fatalf("code %d => %d %+v", bad, code, b)
		}
		if b.Reason != "Bad" || b.Detail != "x" {
			t.Fatal("status fallback must preserve explicitly provided public fields")
		}
	}
	for _, valid := range []int{400, 499, 500, 599} {
		if code, b = Respond(New(valid, "Explicit", "x")); code != valid || b.Status != valid {
			t.Fatalf("explicit code %d => %d %+v", valid, code, b)
		}
	}
}

func TestRespond_DoesNotLeakInternalDetails(t *testing.T) {
	secret := errors.New("dial postgres://user:pass@10.0.0.1:5432")
	for _, input := range []error{
		secret,
		fmt.Errorf("private request /admin/secret: %w", ErrNotFound.Wrap(secret)),
	} {
		_, body := Respond(Normalize(input))
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "postgres") || strings.Contains(string(raw), "/admin/secret") {
			t.Fatalf("leaked: %s", raw)
		}
	}
}

func TestRespond_MetadataDoesNotAliasError(t *testing.T) {
	err := ErrNotFound.KV("field", "user_id")
	_, body := Respond(err)
	body.Metadata["field"] = "changed"
	if err.Metadata()["field"] != "user_id" {
		t.Fatal("response metadata must not mutate the original error")
	}
}

func TestRespond_InternalErrorDetail(t *testing.T) {
	cause := errors.New("storage backend unavailable")
	internal := ErrInternal.WithMessage("query failed on shard 7").Wrap(cause)
	tests := []struct {
		name string
		err  *AppError
		want string
	}{
		{"explicit internal", internal, ErrInternal.Message()},
		{"wrapped internal", Normalize(fmt.Errorf("lookup: %w", internal)), ErrInternal.Message()},
		{"mapper internal", Normalize(cause, func(error) *AppError { return internal }), ErrInternal.Message()},
		{"public outer error", ErrNotFound.Wrap(internal), ErrNotFound.Message()},
		{"domain error with status 500", New(500, "Report.Failed", "报表生成失败"), "报表生成失败"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, body := Respond(tt.err)
			if body.Detail != tt.want {
				t.Errorf("detail = %q, want %q", body.Detail, tt.want)
			}
		})
	}
	if internal.Message() != "query failed on shard 7" || !errors.Is(internal, cause) {
		t.Fatal("responding changed the diagnostic message or error chain")
	}
}

// 与 gin.Context.JSON(code int, obj any) 同形；编译通过即证明
// c.JSON(errcode.Respond(errcode.Normalize(err))) 这一文档化用法成立。
func jsonLike(code int, obj any) (status int, body any) { return code, obj }

func TestRespond_FitsJSONSignature(t *testing.T) {
	if code, _ := jsonLike(Respond(Normalize(ErrNotFound))); code != 404 {
		t.Fatalf("code = %d", code)
	}
}

// 响应包含 Problem Details 标准成员和公开的扩展成员。
func TestBody_JSONShape(t *testing.T) {
	_, b := Respond(ErrNotFound.KV("id", "7").Wrap(errors.New("secret cause")))
	raw, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"title", "status", "detail", "reason", "metadata"} {
		if _, ok := got[k]; !ok {
			t.Errorf("missing member %q in %s", k, raw)
		}
	}
	for _, k := range []string{"code", "message", "cause", "type"} {
		if _, ok := got[k]; ok {
			t.Errorf("unexpected member %q in %s", k, raw)
		}
	}
	if len(got) != 5 {
		t.Errorf("members = %v", got)
	}

	// 非标准状态码没有标准短语：title 省略而不是输出空串。
	_, b = Respond(New(499, "Custom.Canceled", "request canceled"))
	raw, _ = json.Marshal(b)
	if strings.Contains(string(raw), `"title"`) || b.Status != 499 {
		t.Errorf("499 body = %s", raw)
	}
}

func TestContentType(t *testing.T) {
	if ContentType != "application/problem+json" {
		t.Fatalf("ContentType = %q", ContentType)
	}
}

// captureLogs 把默认 slog 暂时指向缓冲区。
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logs bytes.Buffer
	old := slog.Default()
	t.Cleanup(func() { slog.SetDefault(old) })
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	return &logs
}

// 5xx 是服务端故障，记 Error；其余是客户端可预期的失败，记 Warn。
// 日志带请求方法和路径，不带查询串。
func TestAppHandler_LogLevelAndRequestFields(t *testing.T) {
	logs := captureLogs(t)
	tests := []struct {
		name  string
		err   *AppError
		level string
	}{
		{"client error", ErrNotFound, "WARN"},
		{"canceled", ErrCanceled, "WARN"},
		{"server error", ErrInternal.Wrap(errors.New("boom")), "ERROR"},
		{"timeout", ErrTimeout, "ERROR"},
		{"invalid declared status is served as 500", &AppError{}, "ERROR"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs.Reset()
			h := AppHandler(func(http.ResponseWriter, *http.Request) *AppError { return tt.err })
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/users/7?token=secret", http.NoBody))

			var entry struct{ Level, Method, Path string }
			if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
				t.Fatalf("decoding log %q: %v", logs.String(), err)
			}
			if entry.Level != tt.level || entry.Method != http.MethodGet || entry.Path != "/users/7" {
				t.Fatalf("log = %s", logs.String())
			}
			if strings.Contains(logs.String(), "secret") {
				t.Fatalf("query string must not be logged: %s", logs.String())
			}
		})
	}
}

type brokenWriter struct {
	header http.Header
	status int
}

func (w *brokenWriter) Header() http.Header       { return w.header }
func (w *brokenWriter) WriteHeader(code int)      { w.status = code }
func (w *brokenWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

// 写响应失败只可能是连接已断开：不再额外记一条日志（http.Error 同样忽略）。
func TestAppHandler_IgnoresResponseWriteFailure(t *testing.T) {
	logs := captureLogs(t)
	w := &brokenWriter{header: http.Header{}}
	h := AppHandler(func(http.ResponseWriter, *http.Request) *AppError { return ErrNotFound })
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", http.NoBody))

	if w.status != http.StatusNotFound {
		t.Fatalf("status = %d", w.status)
	}
	if n := strings.Count(logs.String(), "\n"); n != 1 || strings.Contains(logs.String(), "broken pipe") {
		t.Fatalf("want only the request-failed entry, got %d lines: %s", n, logs.String())
	}
}

// 405 的 Allow 头由适配器负责；预定义错误的消息风格与其他预定义错误一致。
func TestErrMethodNotAllowed(t *testing.T) {
	status, body := Respond(ErrMethodNotAllowed)
	if status != http.StatusMethodNotAllowed || body.Reason != "MethodNotAllowed" || body.Detail != "Method not allowed." || body.Title != "Method Not Allowed" {
		t.Fatalf("got %d %+v", status, body)
	}
}
