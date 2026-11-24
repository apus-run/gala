package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	gomysql "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"

	"github.com/apus-run/gala/components/authn/store"
	"github.com/apus-run/gala/components/authn/store/storetest"
)

const testPrefix = "authn_"

// sqlClock waits in real time: the SQL store derives expiry from time.Now().
type sqlClock struct{}

func (sqlClock) Advance(d time.Duration) { time.Sleep(d) }

// TestStoreContract runs the shared SessionStorer contract against a real
// MySQL or MariaDB. Set AUTHN_MYSQL_DSN to enable it:
//
//	AUTHN_MYSQL_DSN='root@tcp(127.0.0.1:3306)/' go test ./store/mysql/
//
// Without the DSN the test is skipped rather than silently passing — a skipped
// contract is visible in the output, a mocked one is not.
// TestStoreContract 在真实 MySQL/MariaDB 上运行共享的 SessionStorer 契约。
// 通过 AUTHN_MYSQL_DSN 开启；未设置时跳过而不是静默通过——跳过的契约在输出里
// 可见，用 mock 顶替的则不可见。
func TestStoreContract(t *testing.T) {
	dsn := os.Getenv("AUTHN_MYSQL_DSN")
	if dsn == "" {
		t.Skip("AUTHN_MYSQL_DSN is not set; skipping the MySQL contract suite")
	}
	storetest.SessionStorer(t, storetest.Options{
		New: func(t *testing.T) (store.SessionStorer, storetest.Clock) {
			st, err := NewStore(newTestDatabase(t, dsn), testPrefix)
			require.NoError(t, err)
			return st, sqlClock{}
		},
	})
}

// newTestDatabase creates a throwaway database per case, so tests never touch
// each other or an existing schema.
// newTestDatabase 为每个用例创建一次性数据库，测试之间以及与现有库之间互不影响。
func newTestDatabase(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	cfg, err := gomysql.ParseDSN(dsn)
	require.NoError(t, err)

	// The admin connection needs no database of its own.
	adminCfg := cfg.Clone()
	adminCfg.DBName = ""
	admin, err := sql.Open("mysql", adminCfg.FormatDSN())
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	require.NoError(t, admin.PingContext(ctx))

	name := fmt.Sprintf("authn_test_%d", time.Now().UnixNano())
	_, err = admin.ExecContext(ctx, "CREATE DATABASE `"+name+"`")
	require.NoError(t, err)

	cfg.DBName = name
	// CreateSession's affected-rows semantics depend on this staying off.
	// CreateSession 的影响行数语义依赖该选项保持关闭。
	cfg.ClientFoundRows = false
	db, err := sql.Open("mysql", cfg.FormatDSN())
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = admin.ExecContext(context.Background(), "DROP DATABASE IF EXISTS `"+name+"`")
		_ = db.Close()
		_ = admin.Close()
	})
	require.NoError(t, db.PingContext(ctx))
	st, err := NewStore(db, testPrefix)
	require.NoError(t, err)
	require.NoError(t, st.Migrate(ctx))
	return db
}
