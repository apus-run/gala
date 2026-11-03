package logger

import (
	"log/slog"
	"strings"
)

// Level is slog.Level aliased for this package | Level 是 slog.Level 的包内别名.
type Level = slog.Level

// Leveler is slog.Leveler aliased | Leveler 是 slog.Leveler 的别名.
type Leveler = slog.Leveler

// LevelVar is slog.LevelVar aliased | LevelVar 是 slog.LevelVar 的别名.
type LevelVar = slog.LevelVar

// Log levels; fatal and panic extend past error | 日志级别；fatal 与 panic 排在 error 之后.
const (
	LevelDebug Level = slog.LevelDebug
	LevelInfo  Level = slog.LevelInfo
	LevelWarn  Level = slog.LevelWarn
	LevelError Level = slog.LevelError
	// LevelFatal marks a fatal failure | 致命失败级别.
	LevelFatal Level = slog.LevelError + 4
	// LevelPanic marks a panic | panic 级别.
	LevelPanic Level = slog.LevelError + 8
)

// ParseLevel parses a level name, falling back to info on unknown input
// | ParseLevel 解析级别名，无法识别时回退 info。
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
