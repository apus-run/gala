package repo

import (
	"context"
	"time"

	"gorm.io/gorm/logger"
)

// captureLogger 把执行过的 SQL 收集起来，用来断言生成的语句形状。
type captureLogger struct{ sqls *[]string }

func (l captureLogger) LogMode(logger.LogLevel) logger.Interface { return l }
func (captureLogger) Info(context.Context, string, ...any)       {}
func (captureLogger) Warn(context.Context, string, ...any)       {}
func (captureLogger) Error(context.Context, string, ...any)      {}
func (l captureLogger) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	sql, _ := fc()
	*l.sqls = append(*l.sqls, sql)
}
