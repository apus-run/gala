package token

import (
	"crypto/sha256"
	"encoding/hex"
)

// Hash returns a stable SHA-256 hex digest for token lookup/storage.
func Hash(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}
