package gorm_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	gormio "gorm.io/gorm"

	"github.com/apus-run/gala/components/dlock"
	dlockgorm "github.com/apus-run/gala/components/dlock/gorm"
)

// legacyGORMLock mirrors the pre-token schema so the test can seed a row that
// a real upgrade would leave behind.
// legacyGORMLock 复刻 token 出现之前的表结构，用于模拟真实升级遗留的旧行。
type legacyGORMLock struct {
	ID        uint   `gorm:"primarykey"`
	Name      string `gorm:"unique"`
	OwnerID   string
	ExpiredAt time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (legacyGORMLock) TableName() string { return "gorm_locks" }

func TestInitRepairsLegacyRowAndTakeover(t *testing.T) {
	ctx := context.Background()
	db, err := gormio.Open(
		sqlite.Open(filepath.Join(t.TempDir(), "locks.db")),
		&gormio.Config{TranslateError: true},
	)
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	if err := db.AutoMigrate(&legacyGORMLock{}); err != nil {
		t.Fatalf("AutoMigrate(legacy) error = %v", err)
	}
	if err := db.Create(&legacyGORMLock{
		Name:      "legacy",
		OwnerID:   "old-owner",
		ExpiredAt: time.Now().Add(-time.Minute),
	}).Error; err != nil {
		t.Fatalf("seed legacy row error = %v", err)
	}

	if err := dlockgorm.Init(ctx, db); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	// MySQL/PostgreSQL cannot scan a NULL token into GORM's string field, so
	// Init must repair both new columns before any read.
	var tokenNulls, versionNulls int64
	if err := db.Raw("SELECT COUNT(*) FROM gorm_locks WHERE token IS NULL").Scan(&tokenNulls).Error; err != nil {
		t.Fatalf("count NULL token error = %v", err)
	}
	if err := db.Raw("SELECT COUNT(*) FROM gorm_locks WHERE version IS NULL").Scan(&versionNulls).Error; err != nil {
		t.Fatalf("count NULL version error = %v", err)
	}
	if tokenNulls != 0 || versionNulls != 0 {
		t.Fatalf("NULLs after Init(): token=%d version=%d, want 0", tokenNulls, versionNulls)
	}

	var current dlockgorm.GORMLock
	if err := db.First(&current, "name = ?", "legacy").Error; err != nil {
		t.Fatalf("First() after Init() error = %v", err)
	}
	if current.Token != "" {
		t.Fatalf("legacy token = %q, want empty", current.Token)
	}
	if current.Version != 0 {
		t.Fatalf("legacy version = %d, want 0", current.Version)
	}

	locker, err := dlockgorm.NewLock(ctx, db,
		dlock.WithLockName("legacy"),
		dlock.WithLockTimeout(time.Minute),
	)
	if err != nil {
		t.Fatalf("NewLock() error = %v", err)
	}
	if err := locker.Lock(ctx); err != nil {
		t.Fatalf("Lock() over expired legacy row error = %v", err)
	}

	if err := db.First(&current, "name = ?", "legacy").Error; err != nil {
		t.Fatalf("First() after takeover error = %v", err)
	}
	if current.Token == "" {
		t.Fatal("takeover did not store a token")
	}
	if current.Version != 1 {
		t.Fatalf("version after takeover = %d, want 1", current.Version)
	}

	if err := locker.Refresh(ctx); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if err := locker.Unlock(ctx); err != nil {
		t.Fatalf("Unlock() error = %v", err)
	}

	var gone dlockgorm.GORMLock
	if err := db.First(&gone, "name = ?", "legacy").Error; !errors.Is(err, gormio.ErrRecordNotFound) {
		t.Fatalf("row after Unlock() error = %v, want ErrRecordNotFound", err)
	}
}
