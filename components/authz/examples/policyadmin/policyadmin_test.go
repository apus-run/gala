package policyadmin

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/glebarez/sqlite"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

func TestRunWritesConvergeAcrossAuthorizers(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "rules.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), db, client, "../model.conf"); err != nil {
		t.Fatal(err)
	}
}

func TestWaitForFinalPolicyRequiresRevocations(t *testing.T) {
	for _, stalePath := range []string{"/reports/list", "/reports/archive"} {
		t.Run(stalePath, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
			defer cancel()
			err := waitForFinalPolicy(ctx, func(args ...any) (bool, error) {
				return args[1] == "/articles/1" || args[1] == stalePath, nil
			})
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("stale grant must not converge: %v", err)
			}
		})
	}
}

func TestWaitForFinalPolicyPropagatesEnforceError(t *testing.T) {
	failure := errors.New("policy unavailable")
	err := waitForFinalPolicy(context.Background(), func(args ...any) (bool, error) {
		if args[1] == "/articles/1" {
			return true, nil
		}
		return false, failure
	})
	if !errors.Is(err, failure) {
		t.Fatalf("want enforcement error, got %v", err)
	}
}
