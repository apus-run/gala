package token_test

import (
	"errors"
	"fmt"
	"strings"

	"github.com/golang-jwt/jwt/v5"

	"github.com/apus-run/gala/components/token"
)

type Payload struct {
	SessionID string `json:"sid"`
}

func ExampleJwtManager() {
	manager := token.NewJwtManager[Payload](
		token.WithSecretKey(strings.Repeat("s", 32)),
		token.WithIssuer("svc-a"),
		token.WithAudience("api-a"),
	)

	raw, err := manager.GenerateToken("user-1", Payload{SessionID: "s-1"})
	fmt.Println("sign:", err == nil)

	claims, err := manager.ParseToken(raw)
	fmt.Println("claims:", claims.Subject, claims.Data.SessionID, err)

	// 同一个密钥、另一个服务签的 token：iss/aud 都对不上，两个原因都挂在错误链上
	other := token.NewJwtManager[Payload](
		token.WithSecretKey(strings.Repeat("s", 32)),
		token.WithIssuer("svc-b"),
		token.WithAudience("api-b"),
	)
	rawOther, _ := other.GenerateToken("user-1", Payload{SessionID: "s-2"})
	_, err = manager.ParseToken(rawOther)
	fmt.Println("cross-service:", errors.Is(err, jwt.ErrTokenInvalidIssuer), errors.Is(err, jwt.ErrTokenInvalidAudience))

	// 没配密钥就签不出来：没有内置默认密钥
	_, err = token.NewJwtManager[Payload]().GenerateToken("user-1", Payload{})
	fmt.Println("no secret:", errors.Is(err, token.ErrEmptyJWTSecret))

	// Output:
	// sign: true
	// claims: user-1 s-1 <nil>
	// cross-service: true true
	// no secret: true
}

func ExampleNewTokenGenerator() {
	gen := token.NewTokenGenerator[Payload](
		token.WithTokenStyle(token.TokenStyleSimple),
		token.WithSimpleTokenLength(24),
	)
	raw, err := gen.GenerateToken("user-1", Payload{})
	fmt.Println("simple:", len(raw), err)

	gen = token.NewTokenGenerator[Payload](token.WithTokenStyle("nope"))
	_, err = gen.GenerateToken("user-1", Payload{})
	fmt.Println("unknown style:", errors.Is(err, token.ErrUnsupportedTokenStyle))

	// Output:
	// simple: 24 <nil>
	// unknown style: true
}
