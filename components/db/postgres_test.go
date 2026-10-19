package db

import (
	"context"
	"strings"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestNewDBWithPostgresDialector(t *testing.T) {
	p, err := NewDB(postgres.Open("host=localhost user=test dbname=test sslmode=disable"), &gorm.Config{
		DisableAutomaticPing: true, DryRun: true, SkipDefaultTransaction: true, TranslateError: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := p.Close(); err != nil {
			t.Error(err)
		}
	})
	raw, ok := Unwrap(p)
	if !ok || raw.Dialector.Name() != "postgres" {
		t.Fatal("PostgreSQL dialector not selected")
	}
	if !raw.Config.TranslateError || !raw.Config.DryRun || !raw.Config.DisableAutomaticPing {
		t.Fatal("constructor did not retain configuration")
	}
	type record struct {
		ID   uint
		Name string
	}
	stmt := p.DB(context.Background()).Create(&record{Name: "alice"}).Statement
	if stmt.Error != nil {
		t.Fatal(stmt.Error)
	}
	if query := stmt.SQL.String(); !strings.Contains(query, "$1") || !strings.Contains(query, "RETURNING") {
		t.Fatalf("unexpected PostgreSQL insert: %s", query)
	}
}
