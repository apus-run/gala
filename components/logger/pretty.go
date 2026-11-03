package logger

import (
	"bytes"
	"context"
	"encoding"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/k0kubun/pp/v3"
)

// prettyHandler keeps slog's text header and expands each attribute below it.
// The printer is configured once and shared read-only by derived handlers.
// prettyHandler 保留 slog 的文本头，并把每个属性展开到其下方。
// printer 只配置一次，由派生 handler 只读共享。
type prettyHandler struct {
	opts    slog.HandlerOptions
	printer *pp.PrettyPrinter
	output  *prettyOutput
	groups  []string
	attrs   string
}

type prettyOutput struct {
	mu sync.Mutex
	w  io.Writer
}

// newPrettyHandler builds the pretty handler | newPrettyHandler 构建 pretty handler。
func newPrettyHandler(w io.Writer, opts slog.HandlerOptions, color bool) slog.Handler {
	printer := pp.New()
	printer.SetColoringEnabled(color)
	printer.SetExportedOnly(true)
	return &prettyHandler{
		opts:    opts,
		printer: printer,
		output:  &prettyOutput{w: w},
	}
}

// Enabled implements slog.Handler, honoring the configured minimum level | 实现 slog.Handler，按配置的最低级别判断.
func (h *prettyHandler) Enabled(_ context.Context, level slog.Level) bool {
	minLevel := slog.LevelInfo
	if h.opts.Level != nil {
		minLevel = h.opts.Level.Level()
	}
	return level >= minLevel
}

// Handle implements slog.Handler, rendering the header plus one line per attribute | 实现 slog.Handler，渲染头部并把每个属性展开成一行.
func (h *prettyHandler) Handle(ctx context.Context, record slog.Record) error {
	var buf bytes.Buffer
	// Let slog format and replace the built-in fields, outside user groups.
	// 让 slog 格式化并替换内建字段，位于用户分组之外。
	opts := h.opts
	opts.AddSource = opts.AddSource && record.PC != 0
	header := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	if err := slog.NewTextHandler(&buf, &opts).Handle(ctx, header); err != nil {
		return err
	}
	buf.Truncate(buf.Len() - 1) // Remove the text header's trailing newline. | 去掉文本头末尾换行
	if h.attrs != "" {
		if buf.Len() > 0 {
			buf.WriteByte('\n')
		}
		buf.WriteString(h.attrs)
	}
	record.Attrs(func(attr slog.Attr) bool {
		h.appendAttr(&buf, h.groups, attr)
		return true
	})
	buf.WriteByte('\n')

	// Derived handlers share this lock so multiline records cannot interleave.
	// 派生 handler 共享这把锁，多行记录因此不会交错。
	h.output.mu.Lock()
	defer h.output.mu.Unlock()
	n, err := h.output.w.Write(buf.Bytes())
	if err == nil && n != buf.Len() {
		err = io.ErrShortWrite
	}
	return err
}

// WithAttrs implements slog.Handler, pre-rendering the attrs into derived handlers | 实现 slog.Handler，把属性预渲染进派生 handler.
func (h *prettyHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	var buf bytes.Buffer
	buf.WriteString(h.attrs)
	for _, attr := range attrs {
		h.appendAttr(&buf, h.groups, attr)
	}
	clone := *h
	clone.attrs = buf.String()
	return &clone
}

// WithGroup implements slog.Handler, extending the key prefix | 实现 slog.Handler，扩展键前缀.
func (h *prettyHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	clone := *h
	clone.groups = append(slices.Clone(h.groups), name)
	return &clone
}

func (h *prettyHandler) appendAttr(buf *bytes.Buffer, groups []string, attr slog.Attr) {
	attr.Value = attr.Value.Resolve()
	if attr.Value.Kind() != slog.KindGroup && h.opts.ReplaceAttr != nil {
		attr = h.opts.ReplaceAttr(groups, attr)
		attr.Value = attr.Value.Resolve()
	}
	if attr.Equal(slog.Attr{}) {
		return
	}
	if attr.Value.Kind() == slog.KindGroup {
		if attr.Key != "" {
			groups = append(slices.Clone(groups), attr.Key)
		}
		for _, child := range attr.Value.Group() {
			h.appendAttr(buf, groups, child)
		}
		return
	}

	key := attr.Key
	if len(groups) > 0 {
		key = strings.Join(groups, ".") + "." + key
	}
	if key == "" || strings.IndexFunc(key, func(r rune) bool {
		return r == '=' || r == '"' || r == utf8.RuneError || unicode.IsSpace(r) || !unicode.IsPrint(r)
	}) >= 0 {
		key = strconv.Quote(key)
	}
	if buf.Len() > 0 {
		buf.WriteByte('\n')
	}
	buf.WriteString(key)
	buf.WriteString(" => ")
	_, _ = h.printer.Fprint(buf, prettyValue(attr.Value.Any()))
}

// Preserve a type's explicit text representation before reflecting its fields.
// 在反射字段之前，保留类型自身的显式文本表示。
func prettyValue(value any) (result any) {
	defer func() {
		if err := recover(); err != nil {
			result = fmt.Sprintf("!ERROR:panic in text marshaling: %v", err)
		}
	}()
	switch v := value.(type) {
	case encoding.TextMarshaler:
		text, err := v.MarshalText()
		if err != nil {
			return fmt.Sprintf("!ERROR:%v", err)
		}
		return string(text)
	case fmt.Formatter, error, fmt.Stringer:
		return fmt.Sprintf("%+v", v)
	default:
		return value
	}
}
