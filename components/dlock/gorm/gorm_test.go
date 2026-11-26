package gorm

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

func TestInitRejectsNilDB(t *testing.T) {
	if err := Init(context.Background(), nil); err == nil {
		t.Fatal("Init(nil) error = nil, want error")
	}
}

func TestNewLockRejectsNilDB(t *testing.T) {
	if _, err := NewLock(context.Background(), nil); err == nil {
		t.Fatal("NewLock(nil) error = nil, want error")
	}
}

func TestIsDuplicateEntry(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "mysql duplicate", err: &mysql.MySQLError{Number: 1062}, want: true},
		{name: "wrapped mysql duplicate", err: fmt.Errorf("create: %w", &mysql.MySQLError{Number: 1062}), want: true},
		{name: "mysql other", err: &mysql.MySQLError{Number: 1064}, want: false},
		{name: "postgres duplicate", err: &pgconn.PgError{Code: "23505"}, want: true},
		{name: "wrapped postgres duplicate", err: fmt.Errorf("create: %w", &pgconn.PgError{Code: "23505"}), want: true},
		{name: "postgres other", err: &pgconn.PgError{Code: "42P01"}, want: false},
		{name: "gorm translated", err: gorm.ErrDuplicatedKey, want: true},
		{name: "generic", err: errors.New("boom"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isDuplicateEntry(tt.err); got != tt.want {
				t.Fatalf("isDuplicateEntry(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
