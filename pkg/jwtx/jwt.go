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
	ErrMissingKeyFunc         = errors.New("keyFunc is missing")
	ErrTokenInvalid           = errors.New("token is invalid")
	ErrUnSupportSigningMethod = errors.New("wrong signing method")
	ErrNeedTokenProvider      = errors.New("token provider is missing")
	ErrSignToken              = errors.New("can not sign token. is the key correct")
	ErrGetKey                 = errors.New("can not get key while signing token")
	ErrMissingClaims          = errors.New("claims is missing")
	ErrNoExpiration           = errors.New("claims has no expiration time")
)

// GenerateToken 生成jwt token
func GenerateToken(keyProvider jwt.Keyfunc, opts ...Option) (string, error) {
	o := NewOptions(opts...)

	if keyProvider == nil {
		return "", ErrNeedTokenProvider
	}

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

// ParseToken 解析jwt token
func ParseToken(tokenStr string, keyFunc jwt.Keyfunc, opts ...Option) (token *jwt.Token, err error) {
	o := NewOptions(opts...)
	if keyFunc == nil {
		return nil, ErrMissingKeyFunc
	}

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

	// 过期的、伪造的都算无效 token；%w 留住原因，调用方可用 errors.Is(err, jwt.ErrTokenExpired) 区分过期
	if err != nil || !token.Valid {
		return nil, fmt.Errorf("%w: %w", ErrTokenInvalid, err)
	}

	return token, nil
}

// GenerateJWTSecret 随机生成签名JWT的密钥
func GenerateJWTSecret(length int) []byte {
	key := make([]byte, length)
	_, err := rand.Read(key)
	if err != nil {
		// 如果随机生成失败，使用时间戳作为备选方案
		now := time.Now().UnixNano()
		for i := range length {
			key[i] = byte((now >> (i % 8)) & 0xff)
		}
	}
	return key
}
