package authn

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/apus-run/gala/components/authn/store"
	"github.com/apus-run/gala/components/token"
	"github.com/apus-run/gala/pkg/jwtx"
)

type CustomClaims struct {
	UserID uint64

	// UserAgent 增强安全性，防止token被盗用
	UserAgent string

	jwt.RegisteredClaims
}

const testSecret = "0123456789abcdef0123456789abcdef"

// claimsWith 模拟调用方自己的 claims 工厂：exp/iss 由它填（authn 不填）
func claimsWith(exp time.Time) func() jwt.Claims {
	return func() jwt.Claims {
		return &CustomClaims{
			UserID:    123,
			UserAgent: "test-agent",
			RegisteredClaims: jwt.RegisteredClaims{
				Subject:   "user-1",
				Issuer:    "gala",
				ExpiresAt: jwt.NewNumericDate(exp),
			},
		}
	}
}

// memStore 是内存版吊销名单，省掉真 redis 才能测吊销路径
type memStore struct{ revoked map[string]bool }

func (m *memStore) Set(_ context.Context, accessToken string, _ any, _ time.Duration) error {
	m.revoked[accessToken] = true
	return nil
}

func (m *memStore) Delete(_ context.Context, accessToken string) (bool, error) {
	_, ok := m.revoked[accessToken]
	delete(m.revoked, accessToken)
	return ok, nil
}

func (m *memStore) Check(_ context.Context, accessToken string) (bool, error) {
	return m.revoked[accessToken], nil
}

func (m *memStore) Close() error { return nil }

func TestSign(t *testing.T) {
	ctx := context.Background()

	// 没配 claims 工厂：不签空 token
	if _, err := NewJwtAuth(nil).Sign(ctx); !errors.Is(err, ErrMissingClaims) {
		t.Fatalf("want ErrMissingClaims, got %v", err)
	}

	// 没配密钥：token 层挡下（没有默认密钥）
	j := NewJwtAuth(nil, WithClaims(claimsWith(time.Now().Add(time.Hour))))
	if _, err := j.Sign(ctx); !errors.Is(err, token.ErrEmptyJWTSecret) {
		t.Fatalf("want ErrEmptyJWTSecret, got %v", err)
	}

	// 缺 exp：token 层挡下
	noExp := NewJwtAuth(nil, WithClaims(func() jwt.Claims { return &CustomClaims{} }),
		WithJWTOptions(token.WithSecretKey(testSecret)))
	if _, err := noExp.Sign(ctx); !errors.Is(err, jwtx.ErrNoExpiration) {
		t.Fatalf("want ErrNoExpiration, got %v", err)
	}

	now := time.Now()
	j = NewJwtAuth(nil,
		WithClaims(claimsWith(now.Add(2*time.Hour))),
		WithJWTOptions(token.WithSecretKey(testSecret), token.WithIssuer("gala")),
	)
	tok, err := j.Sign(ctx)
	require.NoError(t, err)
	assert.Equal(t, "Bearer", tok.GetTokenType())
	assert.NotEmpty(t, tok.GetToken())
	assert.InDelta(t, now.Add(2*time.Hour).Unix(), tok.GetExpireAt(), 5, "过期时间取自 claims 的 exp")
	if _, err := tok.EncodeToJSON(); err != nil {
		t.Fatalf("EncodeToJSON: %v", err)
	}
}

func TestParseClaims(t *testing.T) {
	ctx := context.Background()
	j := NewJwtAuth(nil,
		WithClaims(claimsWith(time.Now().Add(time.Hour))),
		WithJWTOptions(token.WithSecretKey(testSecret), token.WithIssuer("gala")),
	)
	tok, err := j.Sign(ctx)
	require.NoError(t, err)

	claims, err := j.ParseClaims(ctx, tok.GetToken())
	require.NoError(t, err)
	cc, ok := claims.(*CustomClaims)
	require.True(t, ok, "解析结果就是工厂产出的类型")
	assert.Equal(t, uint64(123), cc.UserID)
	assert.Equal(t, "user-1", cc.Subject)

	if _, err := j.ParseClaims(ctx, ""); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("空 token: %v", err)
	}

	// 换密钥验不过
	other := NewJwtAuth(nil, WithClaims(claimsWith(time.Now().Add(time.Hour))),
		WithJWTOptions(token.WithSecretKey(strings.Repeat("t", 32))))
	if _, err := other.ParseClaims(ctx, tok.GetToken()); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("换密钥后应当验不过: %v", err)
	}
}

// 校验面的配置来自 token：配了 audience，另一个 aud 的 token 就过不去
func TestParseClaimsEnforcesTokenConfig(t *testing.T) {
	ctx := context.Background()

	mint := NewJwtAuth(nil, WithClaims(func() jwt.Claims {
		return &CustomClaims{RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "gala",
			Audience:  jwt.ClaimStrings{"api-b"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		}}
	}), WithJWTOptions(token.WithSecretKey(testSecret)))
	raw, err := mint.GenerateToken(ctx)
	require.NoError(t, err)

	mine := NewJwtAuth(nil, WithClaims(claimsWith(time.Now().Add(time.Hour))),
		WithJWTOptions(token.WithSecretKey(testSecret), token.WithIssuer("gala"), token.WithAudience("api-a")))
	_, err = mine.ParseClaims(ctx, raw)
	assert.ErrorIs(t, err, jwt.ErrTokenInvalidAudience, "audience 校验应当来自 token 的配置")
	assert.False(t, errors.Is(err, jwt.ErrTokenInvalidIssuer), "iss 一致，不该报 iss 错")
}

// 吊销名单是 authn 自己的那部分：吊销之后同一个 token 必须过不去
func TestDestroyRevokes(t *testing.T) {
	ctx := context.Background()
	s := &memStore{revoked: map[string]bool{}}
	j := NewJwtAuth(s, WithClaims(claimsWith(time.Now().Add(time.Hour))),
		WithJWTOptions(token.WithSecretKey(testSecret), token.WithIssuer("gala")))

	raw, err := j.GenerateToken(ctx)
	require.NoError(t, err)
	if _, err := j.ParseClaims(ctx, raw); err != nil {
		t.Fatalf("吊销前应当能过: %v", err)
	}

	require.NoError(t, j.Destroy(ctx, raw))
	if _, err := j.ParseClaims(ctx, raw); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("吊销后应当过不去: %v", err)
	}

	// 没有存储时 Destroy 报错，而不是静默"成功"（否则调用方以为登出生效了）
	noStore := NewJwtAuth(nil, WithClaims(claimsWith(time.Now().Add(time.Hour))),
		WithJWTOptions(token.WithSecretKey(testSecret), token.WithIssuer("gala")))
	raw2, err := noStore.GenerateToken(ctx)
	require.NoError(t, err)
	assert.ErrorIs(t, noStore.Destroy(ctx, raw2), ErrEmptySessionStore)
}

// 签出已经过期的 token 是调用方的 bug，Sign 不能回报成功
func TestSignRejectsExpiredClaims(t *testing.T) {
	j := NewJwtAuth(nil, WithClaims(claimsWith(time.Now().Add(-time.Hour))),
		WithJWTOptions(token.WithSecretKey(testSecret), token.WithIssuer("gala")))
	if _, err := j.Sign(context.Background()); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("want ErrTokenInvalid, got %v", err)
	}
}

// 解析是写进工厂返回的那个对象里的：返回共享对象时并发请求会互相覆写（A 拿到 B 的身份）。
// 这条把"共享不安全"钉住 —— 返回值就是工厂那个指针，不是副本。
func TestParseClaimsWritesIntoFactoryObject(t *testing.T) {
	ctx := context.Background()
	shared := &CustomClaims{RegisteredClaims: jwt.RegisteredClaims{
		Subject: "user-1", Issuer: "gala", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}}
	j := NewJwtAuth(nil, WithClaims(func() jwt.Claims { return shared }),
		WithJWTOptions(token.WithSecretKey(testSecret), token.WithIssuer("gala")))

	raw, err := j.GenerateToken(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got, err := j.ParseClaims(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.(*CustomClaims) != shared {
		t.Fatal("返回值应当是工厂那个对象本身（共享它就会串号）")
	}
}

func TestEncryptCompare(t *testing.T) {
	hashed, err := jwtx.Encrypt("passw0rd")
	require.NoError(t, err)
	assert.NoError(t, jwtx.Compare(hashed, "passw0rd"))
	assert.Error(t, jwtx.Compare(hashed, "wrong"))
}

// 真实 store（redis）与 authn 接得上：构造 + 签发不碰存储
func TestNewJwtAuthWithStore(t *testing.T) {
	j := NewJwtAuth(store.NewStore(nil, "authn"),
		WithClaims(claimsWith(time.Now().Add(time.Hour))),
		WithJWTOptions(token.WithSecretKey(testSecret)))
	tok, err := j.Sign(context.Background())
	require.NoError(t, err)
	assert.NotEmpty(t, tok.GetToken())
}
