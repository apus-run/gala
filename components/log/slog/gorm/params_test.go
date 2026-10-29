package gorm

import (
	"context"
	"strings"
	"testing"

	gormlib "gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// 默认不让绑定参数的值进 SQL 日志。gorm 在渲染日志 SQL 前调用 ParamsFilter，
// 再用 Explain 把参数内联进 SQL；参数被去掉后，渲染出来的 SQL 里没有值。
func TestParamsAreNotRenderedByDefault(t *testing.T) {
	var _ gormlib.ParamsFilter = (*Logger)(nil)

	secret := "hunter2"
	sql := "INSERT INTO users (name, password) VALUES (?, ?)"

	l := NewLogger()
	fsql, fvars := l.ParamsFilter(context.Background(), sql, "alice", secret)
	rendered := logger.ExplainSQL(fsql, nil, `'`, fvars...)
	if strings.Contains(rendered, secret) || strings.Contains(rendered, "alice") {
		t.Fatalf("bound parameter values reached the rendered SQL: %s", rendered)
	}

	on := NewLogger(WithLogParams(true))
	fsql, fvars = on.ParamsFilter(context.Background(), sql, "alice", secret)
	rendered = logger.ExplainSQL(fsql, nil, `'`, fvars...)
	if !strings.Contains(rendered, secret) {
		t.Fatalf("WithLogParams(true) must keep the values for local debugging: %s", rendered)
	}
}
