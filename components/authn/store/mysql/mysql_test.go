package mysql

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// openTestStore skips unless AUTHN_MYSQL_DSN is set, then returns a store on a
// throwaway database.
// openTestStore 在未设置 AUTHN_MYSQL_DSN 时跳过，否则返回一次性数据库上的 store。
func openTestStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("AUTHN_MYSQL_DSN")
	if dsn == "" {
		t.Skip("AUTHN_MYSQL_DSN is not set; skipping MySQL integration tests")
	}
	return mustStore(t, newTestDatabase(t, dsn))
}

func mustStore(t *testing.T, db DB) *Store {
	t.Helper()
	st, err := NewStore(db, testPrefix)
	require.NoError(t, err)
	return st
}

func TestInvalidTablePrefix(t *testing.T) {
	for _, prefix := range []string{"Bad_", "1bad", "bad-prefix", strings.Repeat("a", 25)} {
		_, err := NewStore(nil, prefix)
		require.Error(t, err, prefix)
	}
	_, err := NewStore(nil, "")
	require.NoError(t, err)
}

func TestEmptyPrefix(t *testing.T) {
	dsn := os.Getenv("AUTHN_MYSQL_DSN")
	if dsn == "" {
		t.Skip("AUTHN_MYSQL_DSN is not set; skipping MySQL integration tests")
	}
	db := newTestDatabase(t, dsn)
	ctx := context.Background()
	st, err := NewStore(db, "")
	require.NoError(t, err)
	require.NoError(t, st.Migrate(ctx))
	ok, err := st.CreateSession(ctx, "sid", "g1", time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, st.DeleteSession(ctx, "sid"))
}

func TestRevocationStore(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.Set(ctx, "tok", "1", time.Minute))
	ok, err := s.Check(ctx, "tok")
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = s.Check(ctx, "other")
	require.NoError(t, err)
	require.False(t, ok)

	deleted, err := s.Delete(ctx, "tok")
	require.NoError(t, err)
	require.True(t, deleted)
	deleted, err = s.Delete(ctx, "tok")
	require.NoError(t, err)
	require.False(t, deleted, "delete must report whether a row existed")

	ok, err = s.Check(ctx, "tok")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestDeleteExpired(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	// Expiry is judged against the Go clock, so a short TTL plus a sleep is
	// enough; both rows below are expired by the time DeleteExpired runs.
	// 过期以 Go 时钟判断，短 TTL 加等待即可；执行 DeleteExpired 时两行都已过期。
	require.NoError(t, s.Set(ctx, "revoked", "1", 10*time.Millisecond))
	_, err := s.CreateSession(ctx, "sid", "g", 10*time.Millisecond)
	require.NoError(t, err)
	time.Sleep(50 * time.Millisecond)

	n, err := s.DeleteExpired(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 2, n)
}

func TestMigrateIsIdempotent(t *testing.T) {
	dsn := os.Getenv("AUTHN_MYSQL_DSN")
	if dsn == "" {
		t.Skip("AUTHN_MYSQL_DSN is not set; skipping MySQL integration tests")
	}
	db := newTestDatabase(t, dsn) // already migrates once
	st := mustStore(t, db)
	require.NoError(t, st.Migrate(context.Background()))
}

func TestCloseIsNoop(t *testing.T) {
	s := openTestStore(t)
	require.NoError(t, s.Close())
	// The handle is still usable after Close: the pool belongs to the caller.
	_, err := s.Check(context.Background(), "tok")
	require.NoError(t, err)
}
