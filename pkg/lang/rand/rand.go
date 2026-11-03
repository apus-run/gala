// Package rand generates random strings, cryptographically secure or
// pseudorandom.
// 包 rand 生成随机字符串，支持密码学安全与伪随机两种方式。
package rand

import (
	cryptoRand "crypto/rand"
	"math/big"
	mathRand "math/rand"
	"time"
)

// defaultRandomAlphabet is the default character set used by RandomString.
// defaultRandomAlphabet 是 RandomString 使用的默认字符集。
const defaultRandomAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

func init() {
	mathRand.New(mathRand.NewSource(time.Now().UnixNano()))
}

// RandomString generates a cryptographically random string with the specified length.
//
// The generated string matches [A-Za-z0-9]+ and it's transparent to URL-encoding.
// RandomString 生成指定长度的密码学安全随机字符串，
// 结果匹配 [A-Za-z0-9]+ 且对 URL 编码透明。
func RandomString(length int) string {
	return RandomStringWithAlphabet(length, defaultRandomAlphabet)
}

// RandomStringWithAlphabet generates a cryptographically random string
// with the specified length and characters set.
//
// It panics if for some reason rand.Int returns a non-nil error.
// RandomStringWithAlphabet 用给定字符集生成指定长度的密码学安全随机字符串；
// 若 crypto/rand.Int 出错则会 panic。
func RandomStringWithAlphabet(length int, alphabet string) string {
	b := make([]byte, length)
	max := big.NewInt(int64(len(alphabet)))

	for i := range b {
		n, err := cryptoRand.Int(cryptoRand.Reader, max)
		if err != nil {
			panic(err)
		}
		b[i] = alphabet[n.Int64()]
	}

	return string(b)
}

// PseudorandomString generates a pseudorandom string with the specified length.
//
// The generated string matches [A-Za-z0-9]+ and it's transparent to URL-encoding.
//
// For a cryptographically random string (but a little bit slower) use RandomString instead.
// PseudorandomString 生成指定长度的伪随机字符串，结果匹配 [A-Za-z0-9]+
// 且对 URL 编码透明；需要密码学安全（稍慢）请改用 RandomString。
func PseudorandomString(length int) string {
	return PseudorandomStringWithAlphabet(length, defaultRandomAlphabet)
}

// PseudorandomStringWithAlphabet generates a pseudorandom string
// with the specified length and characters set.
//
// For a cryptographically random (but a little bit slower) use RandomStringWithAlphabet instead.
// PseudorandomStringWithAlphabet 用给定字符集生成指定长度的伪随机字符串；
// 需要密码学安全（稍慢）请改用 RandomStringWithAlphabet。
func PseudorandomStringWithAlphabet(length int, alphabet string) string {
	b := make([]byte, length)
	max := len(alphabet)

	for i := range b {
		b[i] = alphabet[mathRand.Intn(max)]
	}

	return string(b)
}
