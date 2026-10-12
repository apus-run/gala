package authn

import (
	"context"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/apus-run/gala/components/authn/store"
)

type CustomClaims struct {
	UserID uint64

	// UserAgent 增强安全性，防止token被盗用
	UserAgent string

	jwt.RegisteredClaims
}

func TestGenerateToken(t *testing.T) {
	// 不给 claims factory 时用默认 claims：只带 exp，寿命取 WithExpired
	j := NewJwtAuth(nil, WithExpired(30*time.Minute))

	tokenStr, err := j.GenerateToken(context.Background())
	require.NoError(t, err)

	tok, err := j.ParseToken(context.Background(), tokenStr)
	require.NoError(t, err)
	claims, ok := tok.Claims.(*jwt.RegisteredClaims)
	require.True(t, ok)
	assert.InDelta(t, time.Now().Add(30*time.Minute).Unix(), claims.ExpiresAt.Unix(), 5, "exp 要跟着 WithExpired")

	// 自带 factory 时 exp 由 factory 负责，签出来的必须是它自己的值
	j = NewJwtAuth(nil, WithClaims(func() jwt.Claims {
		return &CustomClaims{RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		}}
	}))
	tokenStr, err = j.GenerateToken(context.Background())
	require.NoError(t, err)
	tok, err = j.ParseToken(context.Background(), tokenStr)
	require.NoError(t, err)
	cc, ok := tok.Claims.(*CustomClaims)
	require.True(t, ok, "解析出来的 claims 类型要和 factory 给的类型一致")
	assert.InDelta(t, time.Now().Add(time.Hour).Unix(), cc.ExpiresAt.Unix(), 5)
}

func TestParseToken(t *testing.T) {
	j := NewJwtAuth(nil)

	_, err := j.ParseToken(context.Background(), "")
	assert.ErrorIs(t, err, ErrTokenInvalid, "空 token")

	_, err = j.ParseToken(context.Background(), "bad.bad.bad")
	assert.ErrorIs(t, err, ErrTokenInvalid, "不是 token 的串")

	tokenStr, err := j.GenerateToken(context.Background())
	require.NoError(t, err)
	_, err = NewJwtAuth(nil, WithKeyFunc(func(*jwt.Token) (any, error) {
		return []byte("another-key"), nil
	})).ParseToken(context.Background(), tokenStr)
	assert.ErrorIs(t, err, ErrTokenInvalid, "换密钥后签名验不过")
}

func TestNewJwtAuth(t *testing.T) {
	headers := make(map[string]any)
	headers["kid"] = "8b5228a5-b3d2-4165-aaac-58a052629846"
	now := time.Now()
	expiresAt := now.Add(2 * time.Hour)
	opts := []Option{
		WithTokenHeader(headers),
		WithExpired(2 * time.Hour),
		WithKeyFunc(func(token *jwt.Token) (any, error) {
			// Verify that the signing method is HMAC.
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, ErrTokenInvalid
			}
			return []byte("moyn8y9abnd7q4zkq2m73yw8tu9j5ixm"), nil
		}),
		WithClaims(func() jwt.Claims {
			return &CustomClaims{
				UserID:    1,
				UserAgent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/99.0.4844.83 Safari/537.36",
				RegisteredClaims: jwt.RegisteredClaims{
					// Issuer = iss,令牌颁发者。它表示该令牌是由谁创建的
					Issuer: "gala",
					// IssuedAt = iat,令牌颁发时的时间戳。它表示令牌是何时被创建的
					IssuedAt: jwt.NewNumericDate(now),
					// ExpiresAt = exp,令牌的过期时间戳。它表示令牌将在何时过期
					ExpiresAt: jwt.NewNumericDate(expiresAt),
					// NotBefore = nbf,令牌的生效时的时间戳。它表示令牌从什么时候开始生效
					NotBefore: jwt.NewNumericDate(now),
					// Subject = sub,令牌的主体。它表示该令牌是关于谁的
					Subject: "",
				},
			}
		}),
	}

	opts = append(opts, WithSigningMethod(jwt.SigningMethodHS256))

	s := store.NewStore(nil, "authn")

	j, err := NewJwtAuth(s, opts...).Sign(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Log(j.GetToken())
}
