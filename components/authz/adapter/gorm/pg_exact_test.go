package gormadapter

import (
	"os"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// 精确删除在真实 Postgres 上同样成立：空字段存成 ”，不是 NULL。
// 设置 GALA_AUTHZ_PG_DSN 才会运行。
func TestRemovePolicyIsExactOnPostgres(t *testing.T) {
	dsn := os.Getenv("GALA_AUTHZ_PG_DSN")
	if dsn == "" {
		t.Skip("set GALA_AUTHZ_PG_DSN to run")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	a, err := NewAdapterByDB(db)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Exec("DELETE FROM casbin_rule").Error
	for _, r := range [][]string{{"bob", "", "GET"}, {"bob", "/y", "GET"}} {
		if err := a.AddPolicy("p", "p", r); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.RemovePolicy("p", "p", []string{"bob", "", "GET"}); err != nil {
		t.Fatal(err)
	}
	if got := countRules(t, a); got != 1 {
		t.Fatalf("%d rules left, want 1", got)
	}
	_ = db.Exec("DELETE FROM casbin_rule").Error
}
