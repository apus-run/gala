package logger

import (
	"log/slog"
	"strings"
)

type Level = slog.Level
type Leveler = slog.Leveler
type LevelVar = slog.LevelVar

const (
	LevelDebug Level = slog.LevelDebug
	LevelInfo  Level = slog.LevelInfo
	LevelWarn  Level = slog.LevelWarn
	LevelError Level = slog.LevelError
	LevelFatal Level = slog.LevelError + 4
	LevelPanic Level = slog.LevelError + 8
)

func ParseLevel(s string) Level {
	if strings.EqualFold(s, "fatal") {
		return LevelFatal
	}
	if strings.EqualFold(s, "panic") {
		return LevelPanic
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(s)); err == nil {
		return level
	}
	return LevelInfo
}
