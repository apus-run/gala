package logger

import (
	"context"
	"log/slog"
	"runtime"
	"time"
)

func SetDefault(l *slog.Logger) {
	slog.SetDefault(l)
}

func Default() *slog.Logger {
	return slog.Default()
}

func With(args ...any) *slog.Logger {
	return Default().With(args...)
}

func WithGroup(name string) *slog.Logger {
	return Default().WithGroup(name)
}

func Handler() slog.Handler {
	return Default().Handler()
}

func Enabled(ctx context.Context, level Level) bool {
	return Default().Enabled(ctx, level)
}

func Debug(msg string, args ...any) {
	log(context.Background(), LevelDebug, msg, args...)
}

func DebugContext(ctx context.Context, msg string, args ...any) {
	log(ctx, LevelDebug, msg, args...)
}

func Info(msg string, args ...any) {
	log(context.Background(), LevelInfo, msg, args...)
}

func InfoContext(ctx context.Context, msg string, args ...any) {
	log(ctx, LevelInfo, msg, args...)
}

func Warn(msg string, args ...any) {
	log(context.Background(), LevelWarn, msg, args...)
}

func WarnContext(ctx context.Context, msg string, args ...any) {
	log(ctx, LevelWarn, msg, args...)
}

func Error(msg string, args ...any) {
	log(context.Background(), LevelError, msg, args...)
}

func ErrorContext(ctx context.Context, msg string, args ...any) {
	log(ctx, LevelError, msg, args...)
}

func Log(ctx context.Context, level Level, msg string, args ...any) {
	log(ctx, level, msg, args...)
}

func LogAttrs(ctx context.Context, level Level, msg string, attrs ...slog.Attr) {
	handler := Default().Handler()
	if !handler.Enabled(ctx, level) {
		return
	}
	var pcs [1]uintptr
	runtime.Callers(2, pcs[:])
	record := slog.NewRecord(time.Now(), level, msg, pcs[0])
	record.AddAttrs(attrs...)
	_ = handler.Handle(ctx, record)
}

func log(ctx context.Context, level Level, msg string, args ...any) {
	handler := Default().Handler()
	if !handler.Enabled(ctx, level) {
		return
	}
	var pcs [1]uintptr
	runtime.Callers(3, pcs[:])
	record := slog.NewRecord(time.Now(), level, msg, pcs[0])
	record.Add(args...)
	_ = handler.Handle(ctx, record)
}
