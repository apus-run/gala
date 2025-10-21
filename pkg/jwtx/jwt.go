package jwtx

import (
	"crypto/rand"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrMissingKeyFunc         = errors.New("keyFunc is missing")
	ErrTokenInvalid           = errors.New("token is invalid")
	ErrUnSupportSigningMethod = errors.New("wrong signing method")
	ErrNeedTokenProvider      = errors.New("token provider is missing")
	ErrSignToken              = errors.New("can not sign token. is the key correct")
	ErrGetKey                 = errors.New("can not get key while signing token")
)

// GenerateToken 生成jwt token
func GenerateToken(keyProvider jwt.Keyfunc, opts ...Option) (string, error) {
	o := Apply(opts...)

	if keyProvider == nil {
		return "", ErrNeedTokenProvider
	}
	token := jwt.NewWithClaims(o.signingMethod, o.claims())
	if o.tokenHeader != nil {
		for k, v := range o.tokenHeader {
			token.Header[k] = v
		}
	}
	key, err := keyProvider(token)
	if err != nil {
		return "", ErrGetKey
	}
	tokenStr, err := token.SignedString(key)
	if err != nil {
		return "", ErrSignToken
	}

	return tokenStr, nil
}

// ParseToken 解析jwt token
func ParseToken(jwtToken string, keyFunc jwt.Keyfunc, opts ...Option) (token *jwt.Token, err error) {
	o := Apply(opts...)
	if keyFunc == nil {
		return nil, ErrMissingKeyFunc
	}

	if o.claims != nil {
		token, err = jwt.ParseWithClaims(jwtToken, o.claims(), keyFunc)
	} else {
		token, err = jwt.Parse(jwtToken, keyFunc)
	}

	// 过期的, 伪造的, 都可以认为是无效token
	if err != nil || !token.Valid {
		return nil, ErrTokenInvalid
	}

	if token.Method != o.signingMethod {
		return nil, ErrUnSupportSigningMethod
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
		for i := 0; i < length; i++ {
			key[i] = byte((now >> (i % 8)) & 0xff)
		}
	}
	return key
}

// Encrypt encrypts the plain text with bcrypt.
func Encrypt(source string) (string, error) {
	hashedBytes, err := bcrypt.GenerateFromPassword([]byte(source), bcrypt.DefaultCost)
	return string(hashedBytes), err
}

// Compare compares the encrypted text with the plain text if it's the same.
func Compare(hashedPassword, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
}
