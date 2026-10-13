package token_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/apus-run/gala/components/token"
	"github.com/apus-run/gala/pkg/jwtx"
	"github.com/golang-jwt/jwt/v5"
)

func TestJwtManagerGenericClaimsRoundTrip(t *testing.T) {
	t.Parallel()
	type payload struct {
		SessionID string `json:"sid"`
	}
	manager := token.NewJwtManager[payload](token.WithSecretKey(strings.Repeat("s", 32)))
	raw, err := manager.GenerateToken("user-1", payload{SessionID: "session-1"})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := manager.ParseToken(raw)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != "user-1" || claims.Data.SessionID != "session-1" {
		t.Fatalf("unexpected claims: %+v", claims)
	}
	if ok, err := manager.VerifyToken(raw); err != nil || !ok {
		t.Fatalf("verify token: ok=%v err=%v", ok, err)
	}
}

func TestJwtManagerCustomClaimsValidation(t *testing.T) {
	t.Parallel()
	type sessionClaims struct {
		jwt.RegisteredClaims
		SessionID string `json:"sid"`
	}
	manager := token.NewJwtManager[sessionClaims](token.WithSecretKey(strings.Repeat("s", 32)))
	now := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	claims := &sessionClaims{
		Issuer: "hurry", Audience: jwt.ClaimStrings{"hurry-api"}, Subject: "user-1",
		ExpiresAt: jwt.NewNumericDate(now.Add(-10 * time.Second)),
		SessionID: "session-1",
	}
	raw, err := manager.GenerateClaims(claims)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, issuer, audience string
		leeway                 time.Duration
		valid                  bool
	}{
		{"valid within leeway", "hurry", "hurry-api", 30 * time.Second, true},
		{"expired", "hurry", "hurry-api", 0, false},
		{"wrong issuer", "other", "hurry-api", 30 * time.Second, false},
		{"wrong audience", "hurry", "other", 30 * time.Second, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parsed := &sessionClaims{}
			err := manager.ParseClaims(raw, parsed,
				jwt.WithIssuer(tc.issuer), jwt.WithAudience(tc.audience), jwt.WithExpirationRequired(),
				jwt.WithTimeFunc(func() time.Time { return now }), jwt.WithLeeway(tc.leeway),
			)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
			if tc.valid && parsed.SessionID != claims.SessionID {
				t.Fatalf("custom claim lost: %+v", parsed)
			}
		})
	}
}

// 签出的 token 必须带 exp 和 jti
func TestGenerateTokenSetsExpAndJTI(t *testing.T) {
	t.Parallel()
	manager := token.NewJwtManager[struct{}](token.WithSecretKey(strings.Repeat("s", 32)))
	raw, err := manager.GenerateToken("user-1", struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := manager.ParseToken(raw)
	if err != nil {
		t.Fatal(err)
	}
	if claims.ExpiresAt == nil || !claims.ExpiresAt.After(time.Now()) {
		t.Fatalf("exp 应该在将来: %v", claims.ExpiresAt)
	}
	if claims.ID == "" {
		t.Fatal("jti 为空")
	}
}

// 配置里声明过的 iss/aud 都由验证方校验：密钥相同但身份/受众不同的 token 不能放行。
// 同一个 token 换个配置结论就该不同，这是参考实现里 aud 白名单那组正反用例的对应物。
func TestJwtManagerEnforcesConfiguredIssuerAudience(t *testing.T) {
	t.Parallel()
	type payload struct{}
	secret := strings.Repeat("s", 32)

	mine := token.NewJwtManager[payload](token.WithSecretKey(secret),
		token.WithIssuer("svc-a"), token.WithAudience("api-a"))
	raw, err := mine.GenerateToken("user-1", payload{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mine.ParseToken(raw); err != nil {
		t.Fatalf("自己签的自己验不过: %v", err)
	}

	mint := func(iss, aud string) string {
		t.Helper()
		raw, err := token.NewJwtManager[payload](token.WithSecretKey(secret),
			token.WithIssuer(iss), token.WithAudience(aud)).GenerateToken("user-1", payload{})
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}

	// aud 对不上：拒 aud，iss 没问题
	if _, err := mine.ParseToken(mint("svc-a", "api-b")); !errors.Is(err, jwt.ErrTokenInvalidAudience) ||
		errors.Is(err, jwt.ErrTokenInvalidIssuer) {
		t.Fatalf("want 只拒 aud, got %v", err)
	}

	// iss 对不上：拒 iss，aud 没问题
	rawWrongIss := mint("svc-b", "api-a")
	if _, err := mine.ParseToken(rawWrongIss); !errors.Is(err, jwt.ErrTokenInvalidIssuer) ||
		errors.Is(err, jwt.ErrTokenInvalidAudience) {
		t.Fatalf("want 只拒 iss, got %v", err)
	}

	// 两个都不同：两个原因都挂在链上
	if _, err := mine.ParseToken(mint("svc-b", "api-b")); !errors.Is(err, jwt.ErrTokenInvalidIssuer) ||
		!errors.Is(err, jwt.ErrTokenInvalidAudience) {
		t.Fatalf("want 两个都拒, got %v", err)
	}

	// 单次调用可以关掉 iss 校验（jwt/v5 对空的期望值不校验）
	if err := mine.ParseClaims(rawWrongIss, &token.Claims[payload]{}, jwt.WithIssuer("")); err != nil {
		t.Fatalf("jwt.WithIssuer(\"\") 应当跳过 iss 校验: %v", err)
	}
}

func TestJwtManagerRejectsBadInput(t *testing.T) {
	t.Parallel()
	type payload struct{}

	// 未知算法报错，不再静默降级成 ES256
	if _, err := token.NewJwtManager[payload](token.WithSecretKey("k"),
		token.WithAlgorithm("RS256")).GenerateToken("u", payload{}); !errors.Is(err, token.ErrUnsupportedAlgorithm) {
		t.Fatalf("want ErrUnsupportedAlgorithm, got %v", err)
	}

	// 没配密钥就签不了：没有内置默认密钥
	if _, err := token.NewJwtManager[payload]().GenerateToken("u", payload{}); !errors.Is(err, token.ErrEmptyJWTSecret) {
		t.Fatalf("want ErrEmptyJWTSecret, got %v", err)
	}

	manager := token.NewJwtManager[payload](token.WithSecretKey("k"))
	if _, err := manager.GenerateToken("  ", payload{}); !errors.Is(err, token.ErrEmptyUserID) {
		t.Fatalf("want ErrEmptyUserID, got %v", err)
	}
	if _, err := manager.ParseToken(""); !errors.Is(err, token.ErrEmptyToken) {
		t.Fatalf("want ErrEmptyToken, got %v", err)
	}
	if _, err := manager.ParseToken("bad.bad.bad"); !errors.Is(err, jwtx.ErrTokenInvalid) {
		t.Fatalf("want jwtx.ErrTokenInvalid, got %v", err)
	}
}

// 漏设 exp 的 claims 在签发前就拦掉，而不是签出永不过期的 token
func TestGenerateClaimsRequiresExpiration(t *testing.T) {
	t.Parallel()
	manager := token.NewJwtManager[struct{}](token.WithSecretKey(strings.Repeat("s", 32)))
	if _, err := manager.GenerateClaims(&jwt.RegisteredClaims{Subject: "user-1"}); !errors.Is(err, jwtx.ErrNoExpiration) {
		t.Fatalf("want ErrNoExpiration, got %v", err)
	}
}

// 坏 token 的错误链：既认得出 jwtx 的哨兵，也认得出 jwt/v5 的具体原因
// kid 等自定义 header 要跟着 token 走；注册声明整体往返。
// 这段原先在 authn 里用 authn 的选项测（WithTokenHeader/WithExpired/WithKeyFunc），
// 那套 JWT 选项已并入 token，覆盖也挪过来。
func TestJwtManagerTokenHeaderAndRegisteredClaims(t *testing.T) {
	t.Parallel()
	manager := token.NewJwtManager[Payload](
		token.WithSecretKey(strings.Repeat("s", 32)),
		token.WithIssuer("gala"),
		token.WithAlgorithm("HS512"),
		token.WithTokenHeader(map[string]any{"kid": "8b5228a5-b3d2-4165-aaac-58a052629846"}),
	)

	now := time.Now()
	issued := &token.Claims[Payload]{
		Data:      Payload{SessionID: "session-1"},
		Issuer:    "gala",
		Subject:   "user-1",
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(2 * time.Hour)),
	}
	raw, err := manager.GenerateClaims(issued)
	if err != nil {
		t.Fatal(err)
	}

	// header 里的 kid 与 alg（算法取配置，不是 HS256 默认值）
	unverified, _, err := jwt.NewParser().ParseUnverified(raw, &token.Claims[Payload]{})
	if err != nil {
		t.Fatal(err)
	}
	if got := unverified.Header["kid"]; got != "8b5228a5-b3d2-4165-aaac-58a052629846" {
		t.Fatalf("kid = %v", got)
	}
	if got := unverified.Header["alg"]; got != "HS512" {
		t.Fatalf("alg = %v", got)
	}

	// 注册声明逐个往返，data 也在
	parsed := &token.Claims[Payload]{}
	if err := manager.ParseClaims(raw, parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Issuer != "gala" || parsed.Subject != "user-1" || parsed.Data.SessionID != "session-1" {
		t.Fatalf("claims 丢了: %+v", parsed)
	}
	if !parsed.ExpiresAt.Time.Equal(issued.ExpiresAt.Time) ||
		!parsed.IssuedAt.Time.Equal(issued.IssuedAt.Time) ||
		!parsed.NotBefore.Time.Equal(issued.NotBefore.Time) {
		t.Fatalf("时间声明对不上: %+v", parsed.RegisteredClaims)
	}
}

func TestJwtManagerParseFailures(t *testing.T) {
	t.Parallel()
	type payload struct{}
	secret := strings.Repeat("s", 32)
	manager := token.NewJwtManager[payload](token.WithSecretKey(secret))
	now := time.Now()

	claimsAt := func(exp, nbf time.Time) *jwt.RegisteredClaims {
		return &jwt.RegisteredClaims{
			Subject:   "user-1",
			Issuer:    token.DefaultIssuer, // 配置默认就在校验 iss
			ExpiresAt: jwt.NewNumericDate(exp),
			NotBefore: jwt.NewNumericDate(nbf),
		}
	}
	mint := func(c jwt.Claims) string {
		t.Helper()
		raw, err := manager.GenerateClaims(c)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}

	// alg=none：把 alg 改成 none、签名段留空的经典攻击样本
	noneAlg, err := jwt.NewWithClaims(jwt.SigningMethodNone, claimsAt(now.Add(time.Hour), now)).
		SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}

	// 结构合法、另一个密钥签的
	otherRaw, err := token.NewJwtManager[payload](token.WithSecretKey(strings.Repeat("t", 32))).
		GenerateClaims(claimsAt(now.Add(time.Hour), now))
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name    string
		raw     string
		wantErr []error
	}{
		{"过期", mint(claimsAt(now.Add(-time.Hour), now)), []error{jwtx.ErrTokenInvalid, jwt.ErrTokenExpired}},
		{"nbf 在未来", mint(claimsAt(now.Add(time.Hour), now.Add(time.Hour))), []error{jwtx.ErrTokenInvalid, jwt.ErrTokenNotValidYet}},
		{"签名不对", otherRaw, []error{jwtx.ErrTokenInvalid, jwt.ErrTokenSignatureInvalid}},
		{"alg=none", noneAlg, []error{jwtx.ErrUnSupportSigningMethod}},
		{"段数不对", "bad.bad.bad", []error{jwtx.ErrTokenInvalid, jwt.ErrTokenMalformed}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := manager.ParseToken(tc.raw); err == nil {
				t.Fatal("应当解析失败")
			} else {
				for _, want := range tc.wantErr {
					if !errors.Is(err, want) {
						t.Fatalf("errors.Is(err, %v) = false, err = %v", want, err)
					}
				}
			}
		})
	}
}

// 换算法必须双向成立：HS512 签的要能用 HS512 验，且只有 HS512 能验
func TestJwtManagerSigningMethodRoundTrip(t *testing.T) {
	t.Parallel()
	type payload struct{}
	secret := strings.Repeat("s", 32)

	hs512 := token.NewJwtManager[payload](token.WithSecretKey(secret), token.WithAlgorithm("HS512"))
	raw, err := hs512.GenerateToken("user-1", payload{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hs512.ParseToken(raw); err != nil {
		t.Fatalf("HS512 签的必须能用 HS512 验: %v", err)
	}

	// 默认 HS256 的 manager 拒它
	hs256 := token.NewJwtManager[payload](token.WithSecretKey(secret))
	if _, err := hs256.ParseToken(raw); !errors.Is(err, jwtx.ErrUnSupportSigningMethod) {
		t.Fatalf("want ErrUnSupportSigningMethod, got %v", err)
	}

	// 算法名大小写无关
	lower := token.NewJwtManager[payload](token.WithSecretKey(secret), token.WithAlgorithm("hs512"))
	if _, err := lower.ParseToken(raw); err != nil {
		t.Fatalf("算法名大小写应当无关: %v", err)
	}
}

// 配置里的 ParserOptions 对每次解析都生效；单次调用的 opts 追加在后，可以覆盖
func TestJwtManagerConfigParserOptions(t *testing.T) {
	t.Parallel()
	type payload struct{}
	secret := strings.Repeat("s", 32)
	now := time.Now()

	// 刚过期 10 秒的 token
	raw, err := token.NewJwtManager[payload](token.WithSecretKey(secret)).GenerateClaims(
		&jwt.RegisteredClaims{Subject: "user-1", Issuer: token.DefaultIssuer,
			ExpiresAt: jwt.NewNumericDate(now.Add(-10 * time.Second))})
	if err != nil {
		t.Fatal(err)
	}

	lenient := token.NewJwtManager[payload](token.WithSecretKey(secret),
		token.WithParserOptions(jwt.WithLeeway(30*time.Second)))
	if _, err := lenient.ParseToken(raw); err != nil {
		t.Fatalf("配置里的 leeway 没生效: %v", err)
	}

	strict := token.NewJwtManager[payload](token.WithSecretKey(secret))
	if _, err := strict.ParseToken(raw); !errors.Is(err, jwt.ErrTokenExpired) {
		t.Fatalf("want ErrTokenExpired, got %v", err)
	}
	if err := strict.ParseClaims(raw, &jwt.RegisteredClaims{}, jwt.WithLeeway(30*time.Second)); err != nil {
		t.Fatalf("单次调用的 opts 没生效: %v", err)
	}

	frozen := token.NewJwtManager[payload](token.WithSecretKey(secret),
		token.WithParserOptions(jwt.WithTimeFunc(func() time.Time { return now.Add(-time.Hour) })))
	if _, err := frozen.ParseToken(raw); err != nil {
		t.Fatalf("配置里的 timeFunc 没生效: %v", err)
	}
}

func TestJwtManagerVerifyToken(t *testing.T) {
	t.Parallel()
	type payload struct{}
	manager := token.NewJwtManager[payload](token.WithSecretKey(strings.Repeat("s", 32)))
	raw, err := manager.GenerateToken("user-1", payload{})
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := manager.VerifyToken(raw); !ok || err != nil {
		t.Fatalf("合法 token: ok=%v err=%v", ok, err)
	}
	if ok, err := manager.VerifyToken("bad.bad.bad"); ok || !errors.Is(err, jwtx.ErrTokenInvalid) {
		t.Fatalf("坏 token: ok=%v err=%v", ok, err)
	}
}

func TestNewRegisteredClaims(t *testing.T) {
	t.Parallel()
	type payload struct{}
	manager := token.NewJwtManager[payload](token.WithSecretKey(strings.Repeat("s", 32)),
		token.WithIssuer("svc-a"), token.WithAudience("api-a", "api-b"), token.WithExpiry(30*time.Minute))
	fixed := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)

	c, err := manager.NewRegisteredClaims("  user-1  ", fixed)
	if err != nil {
		t.Fatal(err)
	}
	if c.Subject != "user-1" {
		t.Fatalf("subject = %q", c.Subject)
	}
	if c.Issuer != "svc-a" || len(c.Audience) != 2 || c.Audience[0] != "api-a" {
		t.Fatalf("iss/aud 没按配置填: %+v", c)
	}
	if !c.ExpiresAt.Time.Equal(fixed.Add(30 * time.Minute)) {
		t.Fatalf("exp = %v", c.ExpiresAt)
	}
	if !c.IssuedAt.Time.Equal(fixed) || !c.NotBefore.Time.Equal(fixed) {
		t.Fatalf("iat/nbf = %v/%v", c.IssuedAt, c.NotBefore)
	}
	if c.ID == "" {
		t.Fatal("jti 为空")
	}

	// now 为零值时取当前时间
	c, err = manager.NewRegisteredClaims("user-1", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if left := time.Until(c.ExpiresAt.Time); left > 30*time.Minute || left < 29*time.Minute {
		t.Fatalf("exp 距现在 %v", left)
	}

	if _, err := manager.NewRegisteredClaims("   ", fixed); !errors.Is(err, token.ErrEmptyUserID) {
		t.Fatalf("want ErrEmptyUserID, got %v", err)
	}

	// Audience 是拷贝：改返回的 claims 不该动到配置
	c.Audience[0] = "changed"
	next, err := manager.NewRegisteredClaims("user-1", fixed)
	if err != nil {
		t.Fatal(err)
	}
	if next.Audience[0] != "api-a" {
		t.Fatalf("audience 被共享了: %v", next.Audience)
	}
}

func TestGenerateJWTSecret(t *testing.T) {
	t.Parallel()
	if got := token.GenerateJWTSecret(32); len(got) != 64 || !isHex(got) {
		t.Fatalf("want 64 位 hex, got %d: %q", len(got), got)
	}
	if got := token.GenerateJWTSecret(0); len(got) != 2*token.JWTSecretLength {
		t.Fatalf("length<=0 按 %d 字节处理, got %d 字符", token.JWTSecretLength, len(got))
	}
	if token.GenerateJWTSecret(32) == token.GenerateJWTSecret(32) {
		t.Fatal("两次生成不该相同")
	}
}

func TestJwtConfigDefaults(t *testing.T) {
	t.Parallel()

	def := token.DefaultJwtConfig()
	if def.SecretKey != "" {
		t.Fatalf("不该有默认密钥: %q", def.SecretKey)
	}
	if def.Issuer != token.DefaultIssuer || def.Algorithm != token.DefaultJWTAlgorithm || def.Expiry != token.DefaultJWTExpiry {
		t.Fatalf("默认值变了: %+v", def)
	}

	// 空值回落到默认
	c := token.NewJwtConfig(token.WithIssuer(""), token.WithAlgorithm(""), token.WithExpiry(0))
	if c.Issuer != token.DefaultIssuer || c.Algorithm != token.DefaultJWTAlgorithm || c.Expiry != token.DefaultJWTExpiry {
		t.Fatalf("空值没有回落: %+v", c)
	}

	// 两份配置互不影响
	a, b := token.NewJwtConfig(), token.NewJwtConfig()
	a.SecretKey = "changed"
	if b.SecretKey != "" {
		t.Fatal("NewJwtConfig 返回了共享的配置")
	}

	// 入参切片是拷贝
	aud := []string{"a"}
	c = token.NewJwtConfig(token.WithAudience(aud...))
	aud[0] = "changed"
	if c.Audience[0] != "a" {
		t.Fatalf("WithAudience 没有拷贝: %v", c.Audience)
	}

	// 整份覆盖
	c = token.NewJwtConfig(token.WithJwtConfig(token.JwtConfig{SecretKey: "k", Issuer: "iss"}))
	if c.SecretKey != "k" || c.Issuer != "iss" || c.Algorithm != token.DefaultJWTAlgorithm {
		t.Fatalf("WithJwtConfig 没有生效: %+v", c)
	}
}
