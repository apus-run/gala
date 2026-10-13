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

func (h *prettyHandler) Enabled(_ context.Context, level slog.Level) bool {
	minLevel := slog.LevelInfo
	if h.opts.Level != nil {
		minLevel = h.opts.Level.Level()
	}
	return level >= minLevel
}

func (h *prettyHandler) Handle(ctx context.Context, record slog.Record) error {
	var buf bytes.Buffer
	// Let slog format and replace the built-in fields, outside user groups.
	opts := h.opts
	opts.AddSource = opts.AddSource && record.PC != 0
	header := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	if err := slog.NewTextHandler(&buf, &opts).Handle(ctx, header); err != nil {
		return err
	}
	buf.Truncate(buf.Len() - 1) // Remove the text header's trailing newline.
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
	h.output.mu.Lock()
	defer h.output.mu.Unlock()
	n, err := h.output.w.Write(buf.Bytes())
	if err == nil && n != buf.Len() {
		err = io.ErrShortWrite
	}
	return err
}

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
