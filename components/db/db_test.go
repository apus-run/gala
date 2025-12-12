package db

import (
	"context"
	"errors"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// DB provider tests
func TestNewDB(t *testing.T) {
	prov, err := NewDB(sqlite.Open(":memory:"))
	if err != nil {
		t.Fatalf("NewDB failed: %v", err)
	}
	if prov == nil {
		t.Fatal("NewDB returned nil provider")
	}
}

func TestUnwrap(t *testing.T) {
	prov, _ := NewDB(sqlite.Open(":memory:"))
	db, ok := Unwrap(prov)
	if !ok {
		t.Fatal("Unwrap failed to recognize provider")
	}

	if db == nil {
		t.Fatal("Unwrap returned nil gorm.DB")
	}
}

func TestNewDBFromConfig(t *testing.T) {
	dsn := "root:123456@tcp(localhost:13306)/test_db?charset=utf8mb4&parseTime=True&loc=Local"
	prov, err := NewDBFromConfig(dsn)
	if err != nil {
		t.Fatalf("NewDBFromConfig failed: %v", err)
	}
	if prov == nil {
		t.Fatal("NewDBFromConfig returned nil provider")
	}
}

func TestProvider_NewSession(t *testing.T) {
	prov, _ := NewDB(sqlite.Open(":memory:"))
	ctx := context.Background()
	db := prov.(*provider).DB(ctx)
	if db == nil {
		t.Fatal("NewSession returned nil")
	}
	if db.Statement.Context != ctx {
		t.Fatal("NewSession did not set context")
	}
}

func TestProvider_Transaction(t *testing.T) {
	prov, _ := NewDB(sqlite.Open(":memory:"))
	ctx := context.Background()
	called := false
	err := prov.(*provider).TX(ctx, func(tx *gorm.DB) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("Transaction failed: %v", err)
	}
	if !called {
		t.Fatal("Transaction did not call function")
	}
}

func TestProvider_Transaction_Error(t *testing.T) {
	prov, _ := NewDB(sqlite.Open(":memory:"))
	ctx := context.Background()
	testErr := errors.New("test error")
	err := prov.(*provider).TX(ctx, func(tx *gorm.DB) error {
		return testErr
	})
	if err == nil || err.Error() != testErr.Error() {
		t.Fatalf("Transaction did not propagate error, got: %v", err)
	}
}

func TestContainWithMasterOpt(t *testing.T) {
	opt := []Option{
		func(o *option) { o.withMaster = true },
	}
	if !ContainWithMasterOpt(opt) {
		t.Fatal("ContainWithMasterOpt should return true")
	}
	opt = []Option{
		func(o *option) { o.withMaster = false },
	}
	if ContainWithMasterOpt(opt) {
		t.Fatal("ContainWithMasterOpt should return false")
	}
}
