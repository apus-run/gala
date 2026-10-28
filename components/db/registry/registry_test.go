package registry

import (
	"sync"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type regModel struct {
	ID   uint
	Name string
}

type badModel struct{ ID chan int }

func openDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestRegistry_Migrate(t *testing.T) {
	db := openDB(t)
	r := NewRegistry()
	r.Register(&regModel{})

	if db.Migrator().HasTable(&regModel{}) {
		t.Fatal("table should not exist before Migrate")
	}
	if err := r.Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if !db.Migrator().HasTable(&regModel{}) {
		t.Fatal("Migrate did not create the table")
	}
}

func TestRegistry_MigrateError(t *testing.T) {
	r := NewRegistry()
	r.Register(&badModel{})
	if err := r.Migrate(openDB(t)); err == nil {
		t.Fatal("Migrate with an unmappable model should fail")
	}
}

func TestRegistry_MigrateEmpty(t *testing.T) {
	if err := NewRegistry().Migrate(openDB(t)); err != nil {
		t.Fatalf("empty registry Migrate = %v, want nil", err)
	}
}

func TestGlobal(t *testing.T) {
	if err := Migrate(openDB(t)); err != nil {
		t.Fatalf("Migrate before any Register = %v, want nil", err)
	}

	db := openDB(t)
	Register(&regModel{})
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if !db.Migrator().HasTable(&regModel{}) {
		t.Fatal("global Migrate did not create the table")
	}
}

// Register 与 Migrate 并发调用没有数据竞争（配合 -race 运行）。
func TestRegistry_ConcurrentRegisterAndMigrate(t *testing.T) {
	db := openDB(t)
	r := NewRegistry()
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(2)
		go func() { defer wg.Done(); r.Register(&regModel{}) }()
		go func() { defer wg.Done(); _ = r.Migrate(db) }()
	}
	wg.Wait()
	if err := r.Migrate(db); err != nil {
		t.Fatal(err)
	}
}
