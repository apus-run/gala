package jwtx_test

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/apus-run/gala/pkg/jwtx"
)

// Claims 是调用方自己的 claims 类型，jwtx 不内置。
type Claims struct {
	UserID uint64 `json:"uid"`
	jwt.RegisteredClaims
}

func keyFunc(*jwt.Token) (any, error) {
	return []byte("0123456789abcdef0123456789abcdef"), nil
}

func Example() {
	claims := &Claims{
		UserID:    42,
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}

	tokenStr, err := jwtx.GenerateToken(keyFunc, jwtx.WithClaims(func() jwt.Claims { return claims }))
	fmt.Println("sign:", err == nil)

	tok, err := jwtx.ParseToken(tokenStr, keyFunc, jwtx.WithClaims(func() jwt.Claims { return &Claims{} }))
	fmt.Println("uid:", tok.Claims.(*Claims).UserID, err)

	// 过期的 token 照样返回 ErrTokenInvalid，但原因可以从错误链上认出来
	expired := &Claims{UserID: 42,
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour))}
	pastStr, _ := jwtx.GenerateToken(keyFunc, jwtx.WithClaims(func() jwt.Claims { return expired }))
	_, err = jwtx.ParseToken(pastStr, keyFunc, jwtx.WithClaims(func() jwt.Claims { return &Claims{} }))
	fmt.Println("expired:", errors.Is(err, jwtx.ErrTokenInvalid), errors.Is(err, jwt.ErrTokenExpired))

	// 没设 exp 的 claims 在签发前就被拦下，不会签出永不过期的凭证
	_, err = jwtx.GenerateToken(keyFunc, jwtx.WithClaims(func() jwt.Claims { return &Claims{} }))
	fmt.Println("no exp:", errors.Is(err, jwtx.ErrNoExpiration))

	// 非预期算法的 token 不进入 keyFunc
	hs512, _ := jwtx.GenerateToken(keyFunc, jwtx.WithSigningMethod(jwt.SigningMethodHS512),
		jwtx.WithClaims(func() jwt.Claims { return claims }))
	_, err = jwtx.ParseToken(hs512, keyFunc, jwtx.WithClaims(func() jwt.Claims { return &Claims{} }))
	fmt.Println("alg:", errors.Is(err, jwtx.ErrUnSupportSigningMethod))

	// Output:
	// sign: true
	// uid: 42 <nil>
	// expired: true true
	// no exp: true
	// alg: true
}
