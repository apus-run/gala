//go:build integration

package db

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type postgresProject struct {
	ID   uint
	Name string `gorm:"uniqueIndex"`
}
type postgresTask struct {
	ID        uint
	ProjectID uint
	Project   postgresProject `gorm:"constraint:OnDelete:RESTRICT"`
	Name      string
}

func TestPostgresProviderIntegration(t *testing.T) {
	dsn := os.Getenv("GALA_DB_POSTGRES_DSN")
	if dsn == "" {
		t.Fatal("set GALA_DB_POSTGRES_DSN for integration tests")
	}
	p, err := NewPostgresFromConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := p.Close(); err != nil {
			t.Error(err)
		}
	})
	ctx := context.Background()
	db := p.DB(ctx)
	if db.Migrator().HasTable(&postgresProject{}) {
		t.Fatal("expected fresh integration database")
	}
	if err := db.AutoMigrate(&postgresProject{}, &postgresTask{}); err != nil {
		t.Fatal(err)
	}
	project := postgresProject{Name: "first"}
	if err := db.Create(&project).Error; err != nil || project.ID == 0 {
		t.Fatalf("insert returning id=%d err=%v", project.ID, err)
	}
	duplicate := postgresProject{Name: "first"}
	if err := db.Create(&duplicate).Error; !errors.Is(err, gorm.ErrDuplicatedKey) {
		t.Fatalf("duplicate translation: %v", err)
	}
	orphan := postgresTask{ProjectID: project.ID + 100, Name: "orphan"}
	if err := db.Create(&orphan).Error; !errors.Is(err, gorm.ErrForeignKeyViolated) {
		t.Fatalf("FK translation: %v", err)
	}
	rollback := errors.New("rollback")
	if err := p.TX(ctx, func(tx *gorm.DB) error {
		if err := p.DB(ctx, WithTransaction(tx)).Create(&postgresProject{Name: "rolled-back"}).Error; err != nil {
			return err
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&postgresProject{}).Where("name = ?", "rolled-back").Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("rollback count=%d err=%v", count, err)
	}
	if err := p.TX(ctx, func(tx *gorm.DB) error {
		var locked postgresProject
		if err := p.DB(ctx, WithTransaction(tx), WithSelectForUpdate()).First(&locked, project.ID).Error; err != nil {
			return err
		}
		return p.DB(ctx, WithTransaction(tx)).Model(&locked).Update("name", "updated").Error
	}); err != nil {
		t.Fatal(err)
	}
	var updated postgresProject
	if err := db.Model(&updated).Where("id = ?", project.ID).Clauses(clause.Returning{}).Updates(map[string]any{"name": "returned"}).Error; err != nil || updated.ID != project.ID || updated.Name != "returned" {
		t.Fatalf("update returning=%+v err=%v", updated, err)
	}
	if err := db.Delete(&updated).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&postgresProject{}, project.ID).Error; !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("delete/read: %v", err)
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	if err := p.DB(timeoutCtx).Exec("SELECT pg_sleep(2)").Error; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("query cancellation: %v", err)
	}
	if err := p.DB(ctx).Exec("SELECT 1").Error; err != nil {
		t.Fatal("connection did not recover", err)
	}
}
