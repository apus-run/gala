// HTTP trigger adapter: maps one POST request to one Registry.Run call.
// HTTP 触发适配器：把一个 POST 请求映射为一次 Registry.Run 调用。

package job

import (
	"errors"
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
)

// httpConfig holds the options configured through [Registry.Handler].
// httpConfig 保存通过 [Registry.Handler] 配置的选项。
type httpConfig struct {
	jobHeader   string
	runIDHeader string
	onError     HTTPErrorHandler
}

// HTTPErrorHandler writes the response for a failed triggered job.
//
// It is called for every error returned by [Registry.Run], including an error
// wrapping ErrNotFound, with no response written yet (the run id header has
// already been set when one was present). The handler owns the response: it
// should write a status and body. Use it to log the full error (a
// [*PanicError] carries the stack) and to map backend sentinels such as
// dlock.ErrLocked to 409/503 so clients can tell retryable lease contention
// from a permanent failure. Do not write the job error to the body unless the
// caller is trusted.
// HTTPErrorHandler 为触发失败的任务写响应。
//
// 每个 [Registry.Run] 返回的错误（包括包装 ErrNotFound 的错误）都会调用它，此时
// 响应尚未写入（存在运行 id 时其请求头已设置）。处理器负责写状态码与响应体。可用
// 它记录完整错误（[*PanicError] 带堆栈），或把后端哨兵错误（如 dlock.ErrLocked）
// 映射为 409/503，让客户端区分可重试的租约竞争与永久失败。除非调用方可信，不要把
// 任务错误写进响应体。
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

// Handler returns an http.Handler that triggers registered jobs.
//
// Only POST is accepted (405 with an Allow header otherwise). The job name
// comes from the job header; a missing or empty name is 400. The request body
// is the payload, the run id header (when present) is copied into the
// invocation and echoed on the response, and all request headers are copied
// into Invocation.Header. Success is 204.
//
// By default an unknown job is 404 (errors.Is ErrNotFound) and any job, lease
// or panic error is 500 with a generic body, so internal details do not leak.
// The generic 500 is indistinguishable from a failed job and drops the error
// (including a [*PanicError] stack), so production callers should install
// [WithHTTPErrorHandler] to log the error and map lease contention to 409/503.
//
// The adapter is deliberately generic: it holds no global registry and owns no
// server. Platform-specific protocols (webhook verification, signed requests,
// job-specific paths) belong in adapters that translate them into this handler
// or call Registry.Run directly.
// Handler 返回触发已注册任务的 http.Handler。
//
// 只接受 POST（其它方法返回 405 并带 Allow 头）。任务名来自任务请求头，缺失或为
// 空返回 400。请求体作为 payload，运行 id 请求头（存在时）复制进 Invocation 并在
// 响应中回显，所有请求头复制进 Invocation.Header。成功返回 204。
//
// 默认情况下未知任务返回 404（errors.Is ErrNotFound），任务、租约或 panic 错误
// 返回 500 且响应体通用，避免泄露内部细节。通用 500 无法区分失败的任务，也会丢掉
// 错误（包括 [*PanicError] 的堆栈），因此生产环境应安装 [WithHTTPErrorHandler]
// 记录错误，并把租约竞争映射为 409/503。
//
// 该适配器刻意保持通用：不持有全局 Registry，也不拥有服务器。平台专属协议
// （webhook 校验、签名请求、按任务划分路径）应放在各自的适配器里，转换成此
// Handler 或直接调用 Registry.Run。
func (r *Registry) Handler(opts ...HTTPOption) http.Handler {
	cfg := httpConfig{
		jobHeader:   defaultJobHeader,
		runIDHeader: defaultRunIDHeader,
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

		payload, err := io.ReadAll(req.Body)
		if err != nil {
			http.Error(w, "job: failed to read request body", http.StatusBadRequest)
			return
		}

		inv := Invocation{
			Name:    name,
			RunID:   req.Header.Get(cfg.runIDHeader),
			Payload: payload,
			Header:  req.Header.Clone(),
		}
		if inv.RunID != "" {
			w.Header().Set(cfg.runIDHeader, inv.RunID)
		}

		if err := r.Run(req.Context(), inv); err != nil {
			if cfg.onError != nil {
				cfg.onError(w, req, err)
				return
			}
			if errors.Is(err, ErrNotFound) {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			http.Error(w, "job: run failed", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	})
}
