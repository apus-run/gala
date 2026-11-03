package slog

import (
	"context"
	"log/slog"
	"runtime/debug"
)

var (
	_        slog.Handler = (*Handler)(nil)
	keyStack              = "stack"
)

type (
	// Handler wraps a slog.Handler, optionally attaching a stack trace to error records and running extra hooks | 包装 slog.Handler，可选用堆栈增强错误记录并执行附加钩子.
	Handler struct {
		// handler is the wrapped slog.Handler | 被包装的 slog.Handler.
		handler slog.Handler
		// disableStackTrace turns off stack traces on error records | 关闭错误记录的堆栈追踪.
		disableStackTrace bool
		// keyStack is the attribute key carrying the stack trace | 携带堆栈追踪的属性键.
		keyStack string
		// funcs are extra hooks run on each record | 每条记录上执行的附加钩子.
		funcs []HandleFunc
	}

	// HandlerOption configures a Handler | 配置 Handler 的选项.
	HandlerOption interface {
		apply(*Handler)
	}

	// HandleFunc is a hook invoked with each log record | 处理每条日志记录的钩子函数.
	HandleFunc func(context.Context, *slog.Record)
)

// HandlerOptionFunc adapts a plain function to HandlerOption. | 将普通函数适配为 HandlerOption。
type HandlerOptionFunc func(*Handler)

func (f HandlerOptionFunc) apply(o *Handler) {
	f(o)
}

// NewHandler wraps hdl and applies opts; wrapping an existing Handler carries over its settings | 包装 hdl 并应用 opts；包装已有 Handler 时沿用其配置.
func NewHandler(hdl slog.Handler, opts ...HandlerOption) slog.Handler {
	nh := &Handler{keyStack: keyStack}
	ch, ok := hdl.(*Handler)
	if ok {
		*nh = *ch
	} else {
		nh.handler = hdl
	}
	for _, opt := range opts {
		nh.Apply(opt)
	}
	return nh
}

func (ch *Handler) clone(hdl slog.Handler) *Handler {
	cloned := new(Handler)
	*cloned = *ch
	cloned.handler = hdl
	return cloned
}

// Enabled reports whether the wrapped handler accepts the level | 报告被包装的 handler 是否接受该级别.
func (ch *Handler) Enabled(ctx context.Context, level slog.Level) bool {
	return ch.handler.Enabled(ctx, level)
}

// Handle runs the stack-trace hook, extra HandleFuncs, then the wrapped handler | 先执行堆栈与附加钩子，再交给被包装的 handler 处理.
func (ch *Handler) Handle(ctx context.Context, r slog.Record) error {
	ch.errorLogWithStackTrack(ctx, &r)
	for _, fn := range ch.funcs {
		fn(ctx, &r)
	}
	return ch.handler.Handle(ctx, r)
}

// WithAttrs implements slog.Handler, keeping the wrapper settings | 实现 slog.Handler，并保留包装层配置.
func (ch *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	hdl := ch.handler.WithAttrs(attrs)
	return ch.clone(hdl)
}

// WithGroup implements slog.Handler, keeping the wrapper settings | 实现 slog.Handler，并保留包装层配置.
func (ch *Handler) WithGroup(name string) slog.Handler {
	hdl := ch.handler.WithGroup(name)
	return ch.clone(hdl)
}

// Apply applies one option in place | 原地应用单个选项.
func (ch *Handler) Apply(opt HandlerOption) {
	opt.apply(ch)
}

func (ch *Handler) errorLogWithStackTrack(ctx context.Context, r *slog.Record) {
	if ch.Enabled(ctx, r.Level) && r.Level == slog.LevelError && !ch.disableStackTrace {
		r.AddAttrs(slog.String(ch.keyStack, string(debug.Stack())))
	}
}

// WithDisableStackTrace enables or disables stack traces on error records | 开启或关闭错误记录的堆栈追踪.
func WithDisableStackTrace(disabled bool) HandlerOption {
	return HandlerOptionFunc(func(ch *Handler) {
		ch.disableStackTrace = disabled
	})
}

// WithStackKey sets the attribute key carrying the stack trace | 设置携带堆栈追踪的属性键.
func WithStackKey(key string) HandlerOption {
	return HandlerOptionFunc(func(ch *Handler) {
		if key == "" {
			key = keyStack
		}
		ch.keyStack = key
	})
}

// WithHandleFunc appends a hook run on each record | 追加一个作用于每条记录的钩子.
func WithHandleFunc(fn HandleFunc) HandlerOption {
	return HandlerOptionFunc(func(h *Handler) {
		h.funcs = append(h.funcs, fn)
	})
}
