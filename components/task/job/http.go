// HTTP trigger adapter: maps one POST request to one Registry.Run call.
// HTTP 触发适配器：把一个 POST 请求映射为一次 Registry.Run 调用。

package job

import (
	"errors"
	"fmt"
	"io"
	"net/http"
)

const (
	// defaultJobHeader carries the registered job name.
	// defaultJobHeader 携带已注册的任务名称。
	defaultJobHeader = "X-Gala-Job-Name"

	// defaultRunIDHeader carries an optional run id and echoes it back.
	// defaultRunIDHeader 携带可选的运行 id，并在响应中回显。
	defaultRunIDHeader = "X-Gala-Job-RunID"

	// defaultMaxBodyBytes caps the request body read as the payload: 10 MiB.
	// defaultMaxBodyBytes 限制作为 payload 读取的请求体大小：10 MiB。
	defaultMaxBodyBytes int64 = 10 << 20
)

// httpConfig holds the options configured through [Registry.Handler].
// httpConfig 保存通过 [Registry.Handler] 配置的选项。
type httpConfig struct {
	jobHeader    string
	runIDHeader  string
	onError      HTTPErrorHandler
	maxBodyBytes int64
}

// HTTPErrorHandler writes the response for a failed triggered job.
//
// It is called for every error returned by [Registry.Run] and for an unknown
// job name, which [Registry.Handler] detects before reading the body and
// reports as an error wrapping ErrNotFound; no response is written yet (the
// run id header has already been set when one was present). Adapter-level
// rejections (non-POST, missing name header, unreadable or oversized body) are
// answered by Handler itself and do not call it. The handler owns the
// response: it should write a status and body. Use it to log the full error (a
// [*PanicError] carries the stack) and to map backend sentinels such as
// dlock.ErrLocked to 409/503 so clients can tell retryable lease contention
// from a permanent failure. Do not write the job error to the body unless the
// caller is trusted.
// HTTPErrorHandler 为触发失败的任务写响应。
//
// 每个 [Registry.Run] 返回的错误都会调用它；未知任务名也由 [Registry.Handler]
// 在读取请求体之前检出，并以包装 ErrNotFound 的错误调用它。此时响应尚未写入
// （存在运行 id 时其请求头已设置）。适配器自身的拒绝（非 POST、缺少名称请求头、
// 请求体不可读或超限）由 Handler 直接应答，不会调用它。处理器负责写状态码与响应
// 体。可用它记录完整错误（[*PanicError] 带堆栈），或把后端哨兵错误（如
// dlock.ErrLocked）映射为 409/503，让客户端区分可重试的租约竞争与永久失败。除非
// 调用方可信，不要把任务错误写进响应体。
type HTTPErrorHandler func(w http.ResponseWriter, req *http.Request, err error)

// WithHTTPErrorHandler replaces the default error mapping of
// [Registry.Handler]: ErrNotFound becomes 404 and every other error a generic
// 500. A nil handler keeps the default.
// WithHTTPErrorHandler 替换 [Registry.Handler] 的默认错误映射：ErrNotFound 返回
// 404，其它错误返回通用的 500。handler 为 nil 时保持默认。
func WithHTTPErrorHandler(h HTTPErrorHandler) HTTPOption {
	return func(c *httpConfig) {
		if h != nil {
			c.onError = h
		}
	}
}

// HTTPOption configures the HTTP adapter, see [Registry.Handler].
// HTTPOption 配置 HTTP 适配器，见 [Registry.Handler]。
type HTTPOption func(*httpConfig)

// WithJobHeader overrides the header that names the job to run (default
// X-Gala-Job-Name). An empty name keeps the default.
// WithJobHeader 覆盖指定任务名称的请求头（默认 X-Gala-Job-Name）。空名称保留默认值。
func WithJobHeader(name string) HTTPOption {
	return func(c *httpConfig) {
		if name != "" {
			c.jobHeader = name
		}
	}
}

// WithRunIDHeader overrides the header that carries and echoes the run id
// (default X-Gala-Job-RunID). An empty name keeps the default.
// WithRunIDHeader 覆盖携带并回显运行 id 的请求头（默认 X-Gala-Job-RunID）。
// 空名称保留默认值。
func WithRunIDHeader(name string) HTTPOption {
	return func(c *httpConfig) {
		if name != "" {
			c.runIDHeader = name
		}
	}
}

// WithMaxBodyBytes caps the request body read as the payload (default 10 MiB,
// 10<<20 bytes). A body over the limit is refused with 413 and the job does
// not run, so Invocation.Payload never exceeds the limit. A non-positive
// limit keeps the default.
// WithMaxBodyBytes 限制作为 payload 读取的请求体大小（默认 10 MiB，即 10<<20
// 字节）。超过上限的请求体返回 413 且任务不运行，因此 Invocation.Payload 绝不会
// 超过上限。非正数保留默认值。
func WithMaxBodyBytes(n int64) HTTPOption {
	return func(c *httpConfig) {
		if n > 0 {
			c.maxBodyBytes = n
		}
	}
}

// Handler returns an http.Handler that triggers registered jobs.
//
// Only POST is accepted (405 with an Allow header otherwise). The job name
// comes from the job header; a missing or empty name is 400. An unknown name
// is 404 before the request body is read, so it never buffers a payload. The
// request body is the payload, the run id header (when present) is copied into
// the invocation and echoed on the response, and all request headers are
// copied into Invocation.Header. Success is 204.
//
// The body is read through [http.MaxBytesReader] and capped at 10 MiB by
// default; [WithMaxBodyBytes] changes the cap, and a non-positive value keeps
// the default. A body over the cap is refused with 413 and the job does not
// run, so Invocation.Payload never exceeds the cap; any other body read
// failure is 400.
//
// By default an unknown job is 404 (errors.Is ErrNotFound) and any job, lease
// or panic error is 500 with a generic body, so internal details do not leak.
// The generic 500 is indistinguishable from a failed job and drops the error
// (including a [*PanicError] stack), so production callers should install
// [WithHTTPErrorHandler] to log the error and map lease contention to 409/503.
// That handler is called for run errors and for an unknown job name, but not
// for the adapter-level rejections above, which Handler answers itself.
//
// The adapter is deliberately generic: it holds no global registry and owns no
// server. Platform-specific protocols (webhook verification, signed requests,
// job-specific paths) belong in adapters that translate them into this handler
// or call Registry.Run directly.
// Handler 返回触发已注册任务的 http.Handler。
//
// 只接受 POST（其它方法返回 405 并带 Allow 头）。任务名来自任务请求头，缺失或为
// 空返回 400。未知任务名在读取请求体之前返回 404，因此不会缓冲 payload。请求体作为
// payload，运行 id 请求头（存在时）复制进 Invocation 并在响应中回显，所有请求头
// 复制进 Invocation.Header。成功返回 204。
//
// 请求体通过 [http.MaxBytesReader] 读取，默认上限 10 MiB；[WithMaxBodyBytes]
// 可修改上限，非正数保留默认值。超过上限返回 413 且任务不运行，因此
// Invocation.Payload 绝不会超过上限；其它读取失败返回 400。
//
// 默认情况下未知任务返回 404（errors.Is ErrNotFound），任务、租约或 panic 错误
// 返回 500 且响应体通用，避免泄露内部细节。通用 500 无法区分失败的任务，也会丢掉
// 错误（包括 [*PanicError] 的堆栈），因此生产环境应安装 [WithHTTPErrorHandler]
// 记录错误，并把租约竞争映射为 409/503。该处理器会被运行错误与未知任务名调用，但
// 不会被上面适配器自身的拒绝调用，那些由 Handler 直接应答。
//
// 该适配器刻意保持通用：不持有全局 Registry，也不拥有服务器。平台专属协议
// （webhook 校验、签名请求、按任务划分路径）应放在各自的适配器里，转换成此
// Handler 或直接调用 Registry.Run。
func (r *Registry) Handler(opts ...HTTPOption) http.Handler {
	cfg := httpConfig{
		jobHeader:    defaultJobHeader,
		runIDHeader:  defaultRunIDHeader,
		maxBodyBytes: defaultMaxBodyBytes,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "job: method not allowed", http.StatusMethodNotAllowed)
			return
		}

		name := req.Header.Get(cfg.jobHeader)
		if name == "" {
			http.Error(w, "job: missing job name header "+cfg.jobHeader, http.StatusBadRequest)
			return
		}

		runID := req.Header.Get(cfg.runIDHeader)
		if runID != "" {
			w.Header().Set(cfg.runIDHeader, runID)
		}

		// Reject an unknown job before touching the body: a request that can
		// never run must not make the adapter buffer an arbitrary payload.
		// The error matches Run's ErrNotFound wording, so a custom error
		// handler keeps seeing the same value.
		// 读取请求体之前先拒绝未知任务：注定不会运行的请求不应让适配器缓冲任意
		// payload。错误文本与 Run 的 ErrNotFound 一致，自定义错误处理器照旧收到相同
		// 的值。
		if !r.has(name) {
			writeRunError(w, req, cfg, fmt.Errorf("%w: %q", ErrNotFound, name))
			return
		}

		// MaxBytesReader enforces the cap while reading, so a declared or
		// chunked length cannot bypass it.
		// MaxBytesReader 在读取过程中强制执行上限，声明长度或分块传输都无法绕过。
		body := http.MaxBytesReader(w, req.Body, cfg.maxBodyBytes)
		payload, err := io.ReadAll(body)
		if err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				http.Error(w, "job: request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "job: failed to read request body", http.StatusBadRequest)
			return
		}

		inv := Invocation{
			Name:    name,
			RunID:   runID,
			Payload: payload,
			Header:  req.Header.Clone(),
		}

		if err := r.Run(req.Context(), inv); err != nil {
			writeRunError(w, req, cfg, err)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	})
}

// writeRunError writes the default response for a failed trigger, or delegates
// to the configured [HTTPErrorHandler]. The response is still untouched when
// it is called.
// writeRunError 为失败的触发写默认响应，或交给配置的 [HTTPErrorHandler]。调用时
// 响应尚未写入。
func writeRunError(w http.ResponseWriter, req *http.Request, cfg httpConfig, err error) {
	if cfg.onError != nil {
		cfg.onError(w, req, err)
		return
	}
	if errors.Is(err, ErrNotFound) {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	http.Error(w, "job: run failed", http.StatusInternalServerError)
}
