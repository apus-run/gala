package token

import (
	"crypto/sha256"
	"encoding/hex"
)

// Hash returns a stable SHA-256 hex digest for token lookup/storage | Hash 返回稳定的 SHA-256 十六进制摘要，用于令牌查找与存储.
func Hash(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}
