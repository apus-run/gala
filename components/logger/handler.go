package logger

import (
	"context"
	"log/slog"
)

// discardHandler drops every record | discardHandler 丢弃所有记录.
type discardHandler struct{}

// Enabled implements slog.Handler, always false | 实现 slog.Handler，恒为 false.
func (discardHandler) Enabled(context.Context, slog.Level) bool { return false }

// Handle implements slog.Handler, dropping the record | 实现 slog.Handler，丢弃记录.
func (discardHandler) Handle(context.Context, slog.Record) error { return nil }

// WithAttrs implements slog.Handler | 实现 slog.Handler.
func (h discardHandler) WithAttrs([]slog.Attr) slog.Handler { return h }

// WithGroup implements slog.Handler | 实现 slog.Handler.
func (h discardHandler) WithGroup(string) slog.Handler { return h }
