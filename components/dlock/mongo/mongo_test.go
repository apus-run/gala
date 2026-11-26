package mongo_test

import (
	"context"
	"testing"

	"github.com/apus-run/gala/components/dlock/mongo"
)

func TestNewLockRejectsNilCollection(t *testing.T) {
	if _, err := mongo.NewLock(context.Background(), nil, nil); err == nil {
		t.Fatal("NewLock(nil, nil) error = nil, want error")
	}
}

func TestInitRejectsNilCollection(t *testing.T) {
	if err := mongo.Init(context.Background(), nil); err == nil {
		t.Fatal("Init(nil) error = nil, want error")
	}
}
