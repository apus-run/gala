package authn

import (
	"encoding/json"
)

// tokenInfo is the default in-memory Token implementation | 默认的内存版 Token 实现.
type tokenInfo struct {
	// Token is the token string | 令牌字符串.
	Token string `json:"token"`

	// Type is the token type | 令牌类型.
	Type string `json:"type"`

	// ExpiresAt is the expiration unix timestamp | 过期时间（unix 时间戳）.
	ExpiresAt int64 `json:"expiresAt"`
}

// GetExpireAt returns the expiration unix timestamp | GetExpireAt 返回过期的 unix 时间戳.
func (t *tokenInfo) GetExpireAt() int64 {
	return t.ExpiresAt
}

// GetToken returns the token string | GetToken 返回令牌字符串.
func (t *tokenInfo) GetToken() string {
	return t.Token
}

// GetTokenType returns the token type | GetTokenType 返回令牌类型.
func (t *tokenInfo) GetTokenType() string {
	return t.Type
}

// EncodeToJSON serializes the token to JSON | EncodeToJSON 将令牌序列化为 JSON.
func (t *tokenInfo) EncodeToJSON() ([]byte, error) {
	return json.Marshal(t)
}
