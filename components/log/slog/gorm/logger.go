// Package gorm adapts slog to the gorm logger interface | 将 slog 适配为 gorm 的日志接口。
package gorm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	log "github.com/apus-run/gala/components/log/slog"
)

// Logger adapts to gorm logger | 适配 gorm 日志。
type Logger struct {
	*log.SlogLogger
	*Config
}

// NewLogger builds a gorm Logger from the given options | 用给定选项构建 gorm 日志器.
func NewLogger(options ...Option) *Logger {
	// Adjust the config.
	// 修改配置
	config := NewOptions(options...)
	l := log.NewLogger(log.WithOptions(func(lopts *log.Options) {
		lopts.LogLevel = config.LogLevel
		lopts.Format = config.Format
		lopts.Writer = config.Writer
		lopts.LogGroup = config.LogGroup
		lopts.LogAttrs = config.LogAttrs
	}))

	return &Logger{
		l,
		config,
	}
}

// ParamsFilter implements gorm.ParamsFilter; gorm calls it before rendering the SQL in logs.
// Returning nil params leaves only placeholders in the rendered SQL, so parameter values never reach the log.
// ParamsFilter 实现 gorm.ParamsFilter：gorm 渲染日志里的 SQL 之前会调用它。
// 返回 nil 参数，渲染出来的 SQL 里只剩占位符，参数值不会进日志。
func (l *Logger) ParamsFilter(_ context.Context, sql string, params ...any) (string, []any) {
	if l.LogParams {
		return sql, params
	}
	return sql, nil
}

// LogMode implements gorm's logger.Interface; this adapter keeps a fixed level | 实现 gorm 的 logger.Interface；该适配器保持固定级别.
func (l *Logger) LogMode(level logger.LogLevel) logger.Interface {
	return l
}

// Info implements gorm's logger.Interface | 实现 gorm 的 logger.Interface.
func (l *Logger) Info(ctx context.Context, msg string, kvs ...any) {
	l.Log(ctx, 5, nil, slog.LevelInfo, msg, kvs...)
}

// Warn implements gorm's logger.Interface | 实现 gorm 的 logger.Interface.
func (l *Logger) Warn(ctx context.Context, msg string, kvs ...any) {
	l.Log(ctx, 5, nil, slog.LevelWarn, msg, kvs...)
}

// Error implements gorm's logger.Interface | 实现 gorm 的 logger.Interface.
func (l *Logger) Error(ctx context.Context, msg string, kvs ...any) {
	l.Log(ctx, 5, nil, slog.LevelError, msg, kvs...)
}

// Trace implements gorm's logger.Interface, logging errors, slow queries and per-query info | 实现 gorm 的 logger.Interface，记录错误、慢查询与逐条查询信息.
func (l *Logger) Trace(ctx context.Context, begin time.Time, fc func() (sql string, rowsAffected int64), err error) {
	elapsed := time.Since(begin)
	sql, rows := fc()
	switch {
	case err != nil && (!l.IgnoreRecordNotFoundError || !errors.Is(err, gorm.ErrRecordNotFound)):
		l.Log(ctx, 5, err, slog.LevelError, "query error",
			slog.String("elapsed", elapsed.String()),
			slog.Int64("rows", rows),
			slog.String("sql", sql),
		)
	case l.SlowThreshold != 0 && elapsed > l.SlowThreshold:
		msg := fmt.Sprintf("slow threshold >= %v", l.SlowThreshold)
		l.Log(ctx, 5, nil, slog.LevelWarn, msg,
			slog.String("elapsed", elapsed.String()),
			slog.Int64("rows", rows),
			slog.String("sql", sql),
		)
	case l.LogInfo:
		l.Log(ctx, 5, nil, slog.LevelInfo, "query info",
			slog.String("elapsed", elapsed.String()),
			slog.Int64("rows", rows),
			slog.String("sql", sql),
		)
	}
}
