package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"

	"github.com/apus-run/gala/components/authn/store"
	"github.com/apus-run/gala/components/authn/store/storetest"
)

const testPrefix = "authn_"

// sqlClock waits in real time: the SQL store derives expiry from time.Now().
type sqlClock struct{}

func (sqlClock) Advance(d time.Duration) { time.Sleep(d) }

// TestStoreContract runs the shared SessionStorer contract against a real
// PostgreSQL. Set AUTHN_POSTGRES_DSN to enable it:
//
//	AUTHN_POSTGRES_DSN='postgres://user:pass@localhost:5432/authn?sslmode=disable' go test ./store/postgres/
//
// Without the DSN the test is skipped rather than silently passing — a skipped
// contract is visible in the output, a mocked one is not.
// TestStoreContract 在真实 PostgreSQL 上运行共享的 SessionStorer 契约。
// 通过 AUTHN_POSTGRES_DSN 开启；未设置时跳过而不是静默通过——跳过的契约在输出里
// 可见，用 mock 顶替的则不可见。
func TestStoreContract(t *testing.T) {
	dsn := os.Getenv("AUTHN_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("AUTHN_POSTGRES_DSN is not set; skipping the PostgreSQL contract suite")
	}
	storetest.SessionStorer(t, storetest.Options{
		New: func(t *testing.T) (store.SessionStorer, storetest.Clock) {
			st, err := NewStore(newTestDatabase(t, dsn), testPrefix)
			require.NoError(t, err)
			return st, sqlClock{}
		},
	})
}

// newTestDatabase returns a connection pinned to a fresh schema. Dropping the
// schema cleans up without touching any other data, so the test cannot damage a
// shared database.
// newTestDatabase 返回固定在新 schema 上的连接。删除 schema 即可清理，不会触碰
// 其他数据，因此测试不可能破坏共享数据库。
func newTestDatabase(t *testing.T, dsn string) *sql.Conn {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	require.NoError(t, db.PingContext(ctx))

	conn, err := db.Conn(ctx)
	require.NoError(t, err)

	schema := fmt.Sprintf("authn_test_%d", time.Now().UnixNano())
	_, err = conn.ExecContext(ctx, "CREATE SCHEMA "+schema)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, "SET search_path TO "+schema)
	require.NoError(t, err)
	st, err := NewStore(conn, testPrefix)
	require.NoError(t, err)
	require.NoError(t, st.Migrate(ctx))

	t.Cleanup(func() {
		_, _ = conn.ExecContext(context.Background(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
		_ = conn.Close()
		_ = db.Close()
	})
	return conn
}
