package sqlname

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestForUnprefixed(t *testing.T) {
	names, err := For("")
	require.NoError(t, err)
	assert.Equal(t, "login_sessions", names.Sessions)
	assert.Equal(t, "login_sessions_expires_at_idx", names.SessionsIdx)
	assert.Equal(t, "revoked_access_tokens", names.Revoked)
	assert.Equal(t, "revoked_access_tokens_expires_at_idx", names.RevokedIdx)
}

func TestForPrefix(t *testing.T) {
	names, err := For("app_")
	require.NoError(t, err)
	assert.Equal(t, "app_login_sessions", names.Sessions)
	assert.Equal(t, "app_login_sessions_expires_at_idx", names.SessionsIdx)
	assert.Equal(t, "app_revoked_access_tokens", names.Revoked)
	assert.Equal(t, "app_revoked_access_tokens_expires_at_idx", names.RevokedIdx)
}

func TestForRejectsUnsafePrefixes(t *testing.T) {
	for _, prefix := range []string{
		"App_",                  // uppercase folds differently per database | 大小写在不同数据库折叠规则不同
		"1app",                  // must not start with a digit | 不能以数字开头
		"app-x",                 // dash would need quoting | 连字符需要引号
		`app"; drop`,            // injection attempt | 注入尝试
		"app.",                  // dot would change the schema | 点号会改 schema
		"app ",                  // trailing space | 尾部空格
		strings.Repeat("a", 25), // over the length cap | 超过长度上限
	} {
		_, err := For(prefix)
		assert.Error(t, err, "prefix %q must be rejected", prefix)
	}
}

func TestForAcceptsLengthBoundary(t *testing.T) {
	_, err := For(strings.Repeat("a", 24))
	require.NoError(t, err, "24 bytes is the documented maximum")

	names, err := For(strings.Repeat("a", 24))
	require.NoError(t, err)
	// The longest derived identifier must stay inside PostgreSQL's 63-byte limit.
	// 最长的派生标识符必须落在 PostgreSQL 的 63 字节限制内。
	assert.LessOrEqual(t, len(names.RevokedIdx), 62)
}
