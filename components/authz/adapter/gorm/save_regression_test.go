package gormadapter

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/casbin/casbin/v3/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestSavePolicyFailureRollsBackDeletion(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "policy.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	pool.SetMaxOpenConns(3)
	a, err := NewAdapterByDB(db)
	if err != nil {
		t.Fatal(err)
	}
	makeModel := func(subject string) model.Model {
		m, err := model.NewModelFromFile("examples/rbac_model.conf")
		if err != nil {
			t.Fatal(err)
		}
		m.AddPolicy("p", "p", []string{subject, "data1", "read"})
		return m
	}
	if err := a.SavePolicy(makeModel("alice")); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TRIGGER reject_policy BEFORE INSERT ON casbin_rule WHEN NEW.v0 = 'fail' BEGIN SELECT RAISE(ABORT, 'injected failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if err := a.SavePolicy(makeModel("fail")); err == nil {
		t.Fatal("expected insert error")
	}
	var rows []CasbinRule
	if err := db.Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].V0 != "alice" {
		t.Fatalf("failed save lost old snapshot: %v", rows)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := a.SavePolicyCtx(ctx, makeModel("bob")); err == nil {
		t.Fatal("cancelled context accepted")
	}
	rows = nil
	if err := db.Find(&rows).Error; err != nil || len(rows) != 1 || rows[0].V0 != "alice" {
		t.Fatalf("cancelled save changed snapshot: %v %v", rows, err)
	}
	if err := a.SavePolicy(makeModel("bob")); err != nil {
		t.Fatal(err)
	}
	rows = nil
	if err := db.Find(&rows).Error; err != nil || len(rows) != 1 || rows[0].V0 != "bob" {
		t.Fatalf("subsequent save failed: %v %v", rows, err)
	}
}

func TestLoadPolicyRejectsEmptyPolicyType(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "malformed.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	adapter, err := NewAdapterByDB(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&CasbinRule{}).Error; err != nil {
		t.Fatal(err)
	}
	m, err := model.NewModelFromFile("examples/rbac_model.conf")
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.LoadPolicy(m); err == nil {
		t.Fatal("empty policy type accepted")
	}
}

func TestSavePolicyCustomTableAcrossBatches(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "batch.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	adapter, err := NewAdapterByDBUseTableName(db, "tenant", "policy")
	if err != nil {
		t.Fatal(err)
	}
	m, err := model.NewModelFromFile("examples/rbac_model.conf")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 1205; i++ {
		m.AddPolicy("p", "p", []string{fmt.Sprintf("subject-%d", i), "data1", "read"})
	}
	if err := adapter.SavePolicy(m); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Table("tenant_policy").Count(&count).Error; err != nil || count != 1205 {
		t.Fatalf("batch save count=%d err=%v", count, err)
	}
	if db.Migrator().HasTable("casbin_rule") {
		t.Fatal("custom-table save created default table")
	}
	m.ClearPolicy()
	if err := adapter.SavePolicy(m); err != nil {
		t.Fatal(err)
	}
	if err := db.Table("tenant_policy").Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("clear count=%d err=%v", count, err)
	}
}
