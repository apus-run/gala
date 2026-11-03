// Package gorm adapts zlog to the gorm logger interface | 将 zlog 适配为 gorm 的日志接口。
package gorm

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/apus-run/gala/components/log/zlog"
	gormlogger "gorm.io/gorm/logger"
)

// Logger adapts to zlog logger | 适配 zlog 日志。
type Logger struct {
	*zlog.ZapLogger
	*Config
}

// NewLogger builds a gorm Logger from the given options | 用给定选项构建 gorm 日志器.
func NewLogger(options ...Option) *Logger {
	// Adjust the config.
	// 修改配置
	config := NewOptions(options...)
	z := zlog.NewLogger(zlog.WithOptions(func(zopts *zlog.Options) {
		zopts.Mode = config.Mode
		zopts.Writer = config.Writer
		zopts.LogLevel = config.LogLevel
		zopts.Format = config.Format
	}))

	return &Logger{
		z,
		config,
	}
}

var (
	infoStr       = "%s[info] "
	warnStr       = "%s[warn] "
	errStr        = "%s[error] "
	traceStr      = "[%s][%.3fms] [rows:%v] %s"
	traceWarnStr  = "%s %s[%.3fms] [rows:%v] %s"
	traceErrStr   = "%s %s[%.3fms] [rows:%v] %s"
	slowThreshold = 200 * time.Millisecond
)

var levelM = map[string]gormlogger.LogLevel{
	"panic": gormlogger.Silent,
	"error": gormlogger.Error,
	"warn":  gormlogger.Warn,
	"info":  gormlogger.Info,
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
func (l *Logger) LogMode(level gormlogger.LogLevel) gormlogger.Interface {
	return l
}

// Info implements gorm's logger.Interface | 实现 gorm 的 logger.Interface.
func (l *Logger) Info(ctx context.Context, msg string, kvs ...any) {
	l.Infof(infoStr+msg, append([]any{fileWithLineNum()}, kvs...)...)
}

// Warn implements gorm's logger.Interface | 实现 gorm 的 logger.Interface.
func (l *Logger) Warn(ctx context.Context, msg string, kvs ...any) {
	l.Warnf(warnStr+msg, append([]any{fileWithLineNum()}, kvs...)...)
}

// Error implements gorm's logger.Interface | 实现 gorm 的 logger.Interface.
func (l *Logger) Error(ctx context.Context, msg string, kvs ...any) {
	l.Errorf(errStr+msg, append([]any{fileWithLineNum()}, kvs...)...)
}

// Trace implements gorm's logger.Interface, logging errors, slow queries and per-query info | 实现 gorm 的 logger.Interface，记录错误、慢查询与逐条查询信息.
func (l *Logger) Trace(ctx context.Context, begin time.Time, fc func() (sql string, rowsAffected int64), err error) {
	if levelM[l.LogLevel] <= gormlogger.Silent {
		return
	}

	elapsed := time.Since(begin)
	switch {
	case err != nil && levelM[l.LogLevel] >= gormlogger.Error:
		sql, rows := fc()
		if rows == -1 {
			l.Errorf(traceErrStr, fileWithLineNum(), err, float64(elapsed.Nanoseconds())/1e6, "-", sql)
		} else {
			l.Errorf(traceErrStr, fileWithLineNum(), err, float64(elapsed.Nanoseconds())/1e6, rows, sql)
		}
	case elapsed > slowThreshold && levelM[l.LogLevel] >= gormlogger.Warn:
		sql, rows := fc()
		slowLog := fmt.Sprintf("SLOW SQL >= %v", slowThreshold)
		if rows == -1 {
			l.Warnf(traceWarnStr, fileWithLineNum(), slowLog, float64(elapsed.Nanoseconds())/1e6, "-", sql)
		} else {
			l.Warnf(traceWarnStr, fileWithLineNum(), slowLog, float64(elapsed.Nanoseconds())/1e6, rows, sql)
		}
	case levelM[l.LogLevel] >= gormlogger.Info:
		sql, rows := fc()
		if rows == -1 {
			l.Infof(traceStr, fileWithLineNum(), float64(elapsed.Nanoseconds())/1e6, "-", sql)
		} else {
			l.Infof(traceStr, fileWithLineNum(), float64(elapsed.Nanoseconds())/1e6, rows, sql)
		}
	}
}

func fileWithLineNum() string {
	for i := 4; i < 15; i++ {
		_, file, line, ok := runtime.Caller(i)

		// if ok && (!strings.HasPrefix(file, gormSourceDir) || strings.HasSuffix(file, "_test.go")) {
		if ok && !strings.HasSuffix(file, "_test.go") {
			dir, f := filepath.Split(file)

			return filepath.Join(filepath.Base(dir), f) + ":" + strconv.FormatInt(int64(line), 10)
		}
	}

	return ""
}
