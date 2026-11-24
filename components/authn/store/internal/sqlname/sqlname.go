// Package sqlname derives and validates the table and index names used by the
// SQL backends. It is internal: callers configure naming through
// postgres.NewStore or mysql.NewStore.
// Package sqlname 为 SQL 后端派生并校验表名与索引名。它是 internal 的：
// 调用方通过 postgres.NewStore 或 mysql.NewStore 配置命名。
package sqlname

import (
	"fmt"
	"regexp"
)

// prefixPattern keeps the prefix safe to interpolate as a SQL identifier.
// prefixPattern 保证前缀可以安全地拼进 SQL 标识符。
var prefixPattern = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// Tables holds the identifiers the SQL backends interpolate.
// Tables 保存 SQL 后端需要拼接的标识符。
type Tables struct {
	Sessions    string
	SessionsIdx string
	Revoked     string
	RevokedIdx  string
}

// For derives the names for a prefix. An empty prefix yields the unprefixed
// names; otherwise the prefix must match [a-z_][a-z0-9_]{0,23}. The pattern
// keeps the prefix from escaping its identifier position, and the length cap
// keeps the longest derived index name inside PostgreSQL's 63-byte identifier
// limit (MySQL's is 64).
// For 按前缀派生名字。空前缀得到无前缀名字；非空前缀必须匹配
// [a-z_][a-z0-9_]{0,23}。该模式防止前缀逃出标识符位置，长度上限保证最长的
// 派生索引名不超过 PostgreSQL 的 63 字节标识符限制（MySQL 为 64）。
func For(prefix string) (Tables, error) {
	if prefix != "" && (len(prefix) > 24 || !prefixPattern.MatchString(prefix)) {
		return Tables{}, fmt.Errorf("table prefix %q must match [a-z_][a-z0-9_]{0,23}", prefix)
	}
	return Tables{
		Sessions:    prefix + "login_sessions",
		SessionsIdx: prefix + "login_sessions_expires_at_idx",
		Revoked:     prefix + "revoked_access_tokens",
		RevokedIdx:  prefix + "revoked_access_tokens_expires_at_idx",
	}, nil
}
