// Package mysql provides database/sql implementations of store.Storer and
// store.SessionStorer backed by MySQL or MariaDB. It depends only on
// database/sql: register a driver (for example github.com/go-sql-driver/mysql)
// and pass the *sql.DB in.
// Package mysql 提供 store.Storer 与 store.SessionStorer 的 MySQL/MariaDB 实现。
// 它只依赖 database/sql：注册驱动（例如 github.com/go-sql-driver/mysql）后把
// *sql.DB 传进来即可。
//
// # Notes | 说明
//
// Rotation is a single conditional UPDATE ... WHERE generation = old AND
// expires_at > now, so concurrent rotations have exactly one winner and can
// never recreate a missing or expired session. CreateSession relies on
// ON DUPLICATE KEY UPDATE affected-rows semantics (0 = live row untouched,
// 1 = inserted, 2 = replaced an expired row), so the driver's clientFoundRows
// option must stay off — that is the default. NewStore takes an optional table
// prefix (empty means none; otherwise it must match [a-z_][a-z0-9_]{0,23}),
// and Migrate is a method on the returned store. Revoked tokens are stored as
// SHA-256 digests, never in the clear. Expired rows are not removed
// automatically; run DeleteExpired periodically. Close is a no-op because the
// *sql.DB belongs to the caller.
// 轮换是一条条件 UPDATE（WHERE generation = old AND expires_at > now），并发轮换
// 只有一个赢家，且绝不重建不存在或已过期的会话。CreateSession 依赖
// ON DUPLICATE KEY UPDATE 的影响行数（0=活跃行未动，1=插入，2=替换过期行），
// 所以驱动的 clientFoundRows 选项必须保持关闭（默认即关闭）。NewStore 接受可选
// 表名前缀（空串表示无前缀；非空须匹配 [a-z_][a-z0-9_]{0,23}），Migrate 是返回的
// store 上的方法。吊销令牌只存 SHA-256 摘要，不落明文。过期行不会自动删除，
// 请定期运行 DeleteExpired。*sql.DB 归调用方所有，所以 Close 是空操作。
//
// # Example | 示例
//
//	import (
//		"database/sql"
//		_ "github.com/go-sql-driver/mysql"
//	)
//
//	db, _ := sql.Open("mysql", dsn)
//	st, _ := mysqlstore.NewStore(db, "")
//	_ = st.Migrate(ctx)
//	auth, _ := authn.NewSessionAuth(st, opts...)
package mysql

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/apus-run/gala/components/authn/store"
	"github.com/apus-run/gala/components/authn/store/internal/sqlname"
)

// DB is the handle the store needs. *sql.DB, *sql.Conn and *sql.Tx all satisfy
// it, so callers can pin a connection or run inside a transaction.
// DB 是 Store 需要的数据库句柄。*sql.DB、*sql.Conn 与 *sql.Tx 都满足它，
// 调用方因此可以固定一条连接或在事务中运行。
type DB interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type queries struct {
	createSession, matchSession, rotateSession, deleteSession string
	setToken, deleteToken, checkToken                         string
	deleteExpiredSessions, deleteExpiredTokens                string
}

func queriesFor(t sqlname.Tables) queries {
	return queries{
		createSession: fmt.Sprintf(`
INSERT INTO %[1]s (id, generation, expires_at)
VALUES (?, ?, ?)
ON DUPLICATE KEY UPDATE
	generation = IF(%[1]s.expires_at <= ?, VALUES(generation), %[1]s.generation),
	expires_at = IF(%[1]s.expires_at <= ?, VALUES(expires_at), %[1]s.expires_at)`, t.Sessions),
		matchSession: fmt.Sprintf(
			`SELECT generation FROM %s WHERE id = ? AND expires_at > ?`, t.Sessions),
		rotateSession: fmt.Sprintf(`
UPDATE %s
	SET generation = ?, expires_at = ?
	WHERE id = ? AND generation = ? AND expires_at > ?`, t.Sessions),
		deleteSession:         fmt.Sprintf(`DELETE FROM %s WHERE id = ?`, t.Sessions),
		setToken:              fmt.Sprintf(`INSERT INTO %s (token_hash, expires_at) VALUES (?, ?) ON DUPLICATE KEY UPDATE expires_at = VALUES(expires_at)`, t.Revoked),
		deleteToken:           fmt.Sprintf(`DELETE FROM %s WHERE token_hash = ?`, t.Revoked),
		checkToken:            fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s WHERE token_hash = ? AND expires_at > ?)`, t.Revoked),
		deleteExpiredSessions: fmt.Sprintf(`DELETE FROM %s WHERE expires_at <= ?`, t.Sessions),
		deleteExpiredTokens:   fmt.Sprintf(`DELETE FROM %s WHERE expires_at <= ?`, t.Revoked),
	}
}

// Store is the MySQL-backed token and session storage
// | 基于 MySQL 的令牌与会话存储.
type Store struct {
	db    DB
	names sqlname.Tables
	q     queries
}

// NewStore creates a Store over an existing handle. tablePrefix is prepended to
// every table and index name; pass "" for unprefixed names. An invalid prefix
// returns an error. The handle is not owned by the Store; close it yourself.
// NewStore 基于已有句柄创建 Store。tablePrefix 会加到所有表名与索引名前；传 ""
// 表示无前缀，非法前缀返回错误。句柄不归 Store 所有，请自行关闭。
func NewStore(db DB, tablePrefix string) (*Store, error) {
	names, err := sqlname.For(tablePrefix)
	if err != nil {
		return nil, err
	}
	return &Store{db: db, names: names, q: queriesFor(names)}, nil
}

var (
	_ store.Storer        = (*Store)(nil)
	_ store.SessionStorer = (*Store)(nil)
)

// Migrate creates the authn tables when absent. Because it is a method, it
// always uses the names NewStore derived. It is idempotent.
// Migrate 在表不存在时创建它们。作为方法，它永远使用 NewStore 派生的名字，
// 可重复执行。
func (s *Store) Migrate(ctx context.Context) error {
	t := s.names
	statements := []string{
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id         VARCHAR(128) NOT NULL,
			generation VARCHAR(128) NOT NULL,
			expires_at DATETIME(6) NOT NULL,
			PRIMARY KEY (id),
			KEY %s (expires_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`, t.Sessions, t.SessionsIdx),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			token_hash BINARY(32) NOT NULL,
			expires_at DATETIME(6) NOT NULL,
			PRIMARY KEY (token_hash),
			KEY %s (expires_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`, t.Revoked, t.RevokedIdx),
	}
	for _, stmt := range statements {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("authn/mysql: migrate: %w", err)
		}
	}
	return nil
}

// CreateSession creates the session generation when absent; an expired row is
// replaced. It reports false when a live session already exists.
// CreateSession 在会话不存在时写入代次；过期行会被替换。活跃会话已存在时返回 false。
func (s *Store) CreateSession(ctx context.Context, id, generation string, ttl time.Duration) (bool, error) {
	if ttl <= 0 {
		return false, store.ErrInvalidTTL
	}
	now := time.Now()
	res, err := s.db.ExecContext(ctx, s.q.createSession,
		id, generation, now.Add(ttl), now, now)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	// 1 = inserted, 2 = replaced an expired row, 0 = a live row was left untouched.
	// 1=插入，2=替换过期行，0=活跃行未被改动。
	return n > 0, nil
}

// MatchSession reports whether the generation still matches the stored one
// | MatchSession 判断代次是否与存储的一致.
func (s *Store) MatchSession(ctx context.Context, id, generation string) (bool, error) {
	var stored string
	err := s.db.QueryRowContext(ctx, s.q.matchSession, id, time.Now()).Scan(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return stored == generation, nil
}

// RotateSession atomically swaps the generation when old matches and the
// session has not expired.
// RotateSession 在 old 匹配且会话未过期时原子换代。
func (s *Store) RotateSession(ctx context.Context, id, old, next string, ttl time.Duration) (bool, error) {
	if ttl < time.Millisecond {
		return false, store.ErrTTLTooSmall
	}
	now := time.Now()
	res, err := s.db.ExecContext(ctx, s.q.rotateSession,
		next, now.Add(ttl), id, old, now)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// DeleteSession removes the whole session | DeleteSession 删除整个会话.
func (s *Store) DeleteSession(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, s.q.deleteSession, id)
	return err
}

// tokenHash keeps bearer tokens out of the database.
// tokenHash 让 bearer 令牌不以明文落库。
func tokenHash(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// Set records the token in the revocation list. The opaque value is not
// persisted: no getter exists, so only existence and expiry are observable.
// Set 把令牌记入吊销名单。接口没有读回入口，只有存在性与过期可观察，
// 因此不持久化那个不透明的值。
func (s *Store) Set(ctx context.Context, accessToken string, _ any, expiration time.Duration) error {
	_, err := s.db.ExecContext(ctx, s.q.setToken,
		tokenHash(accessToken), time.Now().Add(expiration))
	return err
}

// Delete removes the token from the revocation list | Delete 从吊销名单删除令牌.
func (s *Store) Delete(ctx context.Context, accessToken string) (bool, error) {
	res, err := s.db.ExecContext(ctx, s.q.deleteToken, tokenHash(accessToken))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// Check reports whether the token is on the revocation list
// | Check 判断令牌是否在吊销名单中.
func (s *Store) Check(ctx context.Context, accessToken string) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx, s.q.checkToken, tokenHash(accessToken), time.Now()).Scan(&exists)
	return exists, err
}

// Close is a no-op: the *sql.DB is owned by the caller.
// Close 是空操作：*sql.DB 归调用方所有。
func (s *Store) Close() error {
	return nil
}

// DeleteExpired removes expired sessions and revocation entries and returns how
// many rows were deleted. Run it periodically; queries already ignore expired
// rows, so this is housekeeping, not correctness.
// DeleteExpired 删除过期的会话与吊销记录并返回删除行数。请定期运行；
// 查询本身已忽略过期行，所以它只负责清理空间，不影响正确性。
func (s *Store) DeleteExpired(ctx context.Context) (int64, error) {
	now := time.Now()
	var total int64
	for _, stmt := range []string{s.q.deleteExpiredSessions, s.q.deleteExpiredTokens} {
		res, err := s.db.ExecContext(ctx, stmt, now)
		if err != nil {
			return total, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}
