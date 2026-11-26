// Package token builds the opaque ownership values stored by dlock backends |
// token 生成 dlock 后端存储的不可伪造所有权令牌.
//
// A token is "<ownerID>@<32 hex chars>". OwnerID is decoration for error
// messages; the random suffix is what proves ownership. Sharing an OwnerID
// between processes is safe because every Locker gets its own suffix.
// 令牌格式为 "<ownerID>@<32 位十六进制>"。OwnerID 仅用于错误信息，随机后缀才是
// 所有权凭据；多个进程共用同一 OwnerID 也不会互相误认。
package token

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
)

const separator = "@"

// New returns a fresh ownership token for ownerID.
// New 为 ownerID 生成新的所有权令牌。
func New(ownerID string) (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return ownerID + separator + hex.EncodeToString(b[:]), nil
}

// Owner extracts the diagnostic owner part of value. A value without a
// separator is returned unchanged so legacy or foreign values stay readable.
// Owner 取出令牌中的持有者标识；没有分隔符的值原样返回，便于展示旧格式的值。
func Owner(value string) string {
	if i := strings.LastIndex(value, separator); i >= 0 {
		return value[:i]
	}
	return value
}
