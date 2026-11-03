// Package jwtx issues and parses JWTs on top of golang-jwt/jwt/v5, with
// bcrypt password helpers | 基于 golang-jwt/jwt/v5 签发与解析 JWT，附带
// bcrypt 密码工具.
//
// # Notes | 说明
//
// Any jwt.SigningMethod can be set with WithSigningMethod; the default is
// HS256. GenerateToken refuses claims without an exp (ErrNoExpiration).
// ParseToken accepts only the one configured signing method and checks it
// before the caller's key function runs, so a token with an unexpected alg
// never reaches key lookup; failures are wrapped as ErrTokenInvalid with the
// cause preserved, so errors.Is(err, jwt.ErrTokenExpired) still works. Keys,
// storage, rotation and refresh flows are the caller's job, expressed
// through jwt.Keyfunc; there is no JWKS or kid support. GenerateJWTSecret
// falls back to a timestamp-derived key when crypto/rand fails, which is
// weak — treat it as best-effort.
// 任意 jwt.SigningMethod 都可通过 WithSigningMethod 设置，默认 HS256。
// GenerateToken 拒绝签发没有 exp 的 claims（ErrNoExpiration）。ParseToken
// 只接受配置的那一种签名算法，并在调用方的 key 函数之前校验，非预期 alg
// 的 token 不会进入密钥查找；失败包装为 ErrTokenInvalid 并保留原因，
// errors.Is(err, jwt.ErrTokenExpired) 依旧可用。密钥、存储、轮换与刷新流程
// 由调用方通过 jwt.Keyfunc 承担，没有 JWKS 与 kid 支持。GenerateJWTSecret
// 在 crypto/rand 失败时回退到时间戳派生密钥，强度弱，仅属尽力而为。
//
// # Features | 功能
//
// - GenerateToken/ParseToken with sentinel errors | 签发/解析，配哨兵错误
// - WithClaims, WithTokenHeader, WithParserOptions pass through jwt/v5 | 透传 jwt/v5 的 claims/头/解析选项
// - GenerateJWTSecret for random secrets | 随机生成密钥
// - Encrypt/Compare bcrypt password hashing | bcrypt 加密与比对
//
// # Example | 示例
//
//	key := func(*jwt.Token) (any, error) { return secret, nil }
//	tok, err := jwtx.GenerateToken(key,
//		jwtx.WithClaims(func() jwt.Claims { return claims }))
//	parsed, err := jwtx.ParseToken(tok, key)
package jwtx

import (
	"crypto/rand"
	"errors"
	"fmt"
	"maps"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var (
	// ErrMissingKeyFunc is returned when no key function is supplied
	// ErrMissingKeyFunc 在未提供 key 函数时返回.
	ErrMissingKeyFunc = errors.New("keyFunc is missing")
	// ErrTokenInvalid wraps every parse or validation failure, cause preserved
	// ErrTokenInvalid 包装所有解析或校验失败，并保留原因.
	ErrTokenInvalid = errors.New("token is invalid")
	// ErrUnSupportSigningMethod is returned for a token with an unexpected alg
	// ErrUnSupportSigningMethod 在 token 使用非预期签名算法时返回.
	ErrUnSupportSigningMethod = errors.New("wrong signing method")
	// ErrNeedTokenProvider is returned when keyProvider is nil
	// ErrNeedTokenProvider 在 keyProvider 为 nil 时返回.
	ErrNeedTokenProvider = errors.New("token provider is missing")
	// ErrSignToken is returned when the token cannot be signed
	// ErrSignToken 在令牌无法签名时返回.
	ErrSignToken = errors.New("can not sign token. is the key correct")
	// ErrGetKey is returned when the key function fails during signing
	// ErrGetKey 在签名时 key 函数失败返回.
	ErrGetKey = errors.New("can not get key while signing token")
	// ErrMissingClaims is returned when no claims are supplied
	// ErrMissingClaims 在未提供 claims 时返回.
	ErrMissingClaims = errors.New("claims is missing")
	// ErrNoExpiration is returned when claims carry no exp
	// ErrNoExpiration 在 claims 没有 exp 时返回.
	ErrNoExpiration = errors.New("claims has no expiration time")
)

// GenerateToken signs claims into a JWT using the given key provider
// GenerateToken 使用给定密钥函数把 claims 签发为 JWT.
func GenerateToken(keyProvider jwt.Keyfunc, opts ...Option) (string, error) {
	o := NewOptions(opts...)

	if keyProvider == nil {
		return "", ErrNeedTokenProvider
	}

	// never sign a token without exp: jwt/v5 does not require exp by default,
	// so a forgotten one would issue credentials that never expire
	// 不签没有 exp 的 token：jwt/v5 默认不要求 exp，漏设就是签出永不过期的凭证
	claims := o.claims()
	if claims == nil {
		return "", ErrMissingClaims
	}
	exp, err := claims.GetExpirationTime()
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrNoExpiration, err)
	}
	if exp == nil {
		return "", ErrNoExpiration
	}

	token := jwt.NewWithClaims(o.signingMethod, claims)
	maps.Copy(token.Header, o.tokenHeader)
	key, err := keyProvider(token)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrGetKey, err)
	}
	tokenStr, err := token.SignedString(key)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrSignToken, err)
	}

	return tokenStr, nil
}

// ParseToken parses and verifies a JWT string with the given key function
// ParseToken 用给定密钥函数解析并校验 JWT 字符串.
func ParseToken(tokenStr string, keyFunc jwt.Keyfunc, opts ...Option) (token *jwt.Token, err error) {
	o := NewOptions(opts...)
	if keyFunc == nil {
		return nil, ErrMissingKeyFunc
	}

	// pin the algorithm before the key is looked up: a token with an
	// unexpected alg never reaches keyFunc and never gets verified with it
	// 取 key 前先卡算法：非预期 alg 的 token 不进 keyFunc，也不参与验签
	keyProvider := func(t *jwt.Token) (any, error) {
		if t.Method != o.signingMethod {
			return nil, ErrUnSupportSigningMethod
		}
		return keyFunc(t)
	}

	parser := jwt.NewParser(o.parserOptions...)
	if o.claims != nil {
		token, err = parser.ParseWithClaims(tokenStr, o.claims(), keyProvider)
	} else {
		token, err = parser.Parse(tokenStr, keyProvider)
	}

	// expired and forged tokens are both invalid; %w keeps the cause, so
	// callers can still tell expiry via errors.Is(err, jwt.ErrTokenExpired)
	// 过期的、伪造的都算无效 token；%w 留住原因，调用方可用 errors.Is(err, jwt.ErrTokenExpired) 区分过期
	if err != nil || !token.Valid {
		return nil, fmt.Errorf("%w: %w", ErrTokenInvalid, err)
	}

	return token, nil
}

// GenerateJWTSecret generates a random secret of the given byte length
// GenerateJWTSecret 随机生成指定字节长度的 JWT 签名密钥.
func GenerateJWTSecret(length int) []byte {
	key := make([]byte, length)
	_, err := rand.Read(key)
	if err != nil {
		// fall back to a timestamp-derived key if random generation fails
		// 如果随机生成失败，使用时间戳作为备选方案
		now := time.Now().UnixNano()
		for i := range length {
			key[i] = byte((now >> (i % 8)) & 0xff)
		}
	}
	return key
}
