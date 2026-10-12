package jwtx

import (
	"log/slog"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type CustomClaims struct {
	UserID uint64

	// UserAgent 增强安全性，防止token被盗用
	UserAgent string

	jwt.RegisteredClaims
}

const testKey = "testKey"

// 固定密钥 testKey 签出的 golden token，喂各种边界。claims 见 testClaims()，
// 改了 claims 可以在 jwt.io 上按同一密钥重算；noneAlg 例外，它是把 alg 改成 none、
// 签名段留空的攻击样本，无密钥可签。
var (
	testJwtValid   = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJVc2VySUQiOjEyMywiVXNlckFnZW50IjoidGVzdC1hZ2VudCIsImlzcyI6ImdhbGEiLCJzdWIiOiJhY2Nlc3MiLCJhdWQiOlsidGVzdF9hdWQiXSwiZXhwIjoyNzg5MTkxODIyLCJuYmYiOjE1MjY4ODQyMjIsImlhdCI6MTUyNjg4NDIyMiwianRpIjoianRpLTEifQ.HDHW98_dDPSETIwWZdz-j8BwBa1JeeipEIYsXfrwKdY"
	testJwtExpired = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJVc2VySUQiOjEyMywiVXNlckFnZW50IjoidGVzdC1hZ2VudCIsImlzcyI6ImdhbGEiLCJzdWIiOiJhY2Nlc3MiLCJhdWQiOlsidGVzdF9hdWQiXSwiZXhwIjoxNTI2ODg3ODIyLCJuYmYiOjE1MjY4ODQyMjIsImlhdCI6MTUyNjg4NDIyMiwianRpIjoianRpLTEifQ.UuudfcmWcEUQBez7hFYPXRM1ZrpWinq7sth8-8p-dMk"
	testJwtNbf     = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJVc2VySUQiOjEyMywiVXNlckFnZW50IjoidGVzdC1hZ2VudCIsImlzcyI6ImdhbGEiLCJzdWIiOiJhY2Nlc3MiLCJhdWQiOlsidGVzdF9hdWQiXSwiZXhwIjoyNzg5MTkxODIyLCJuYmYiOjk5OTk4ODQyMjUsImlhdCI6MTUyNjg4NDIyMiwianRpIjoianRpLTEifQ.xlaGLtDErkevlV1aZ0IJaluAc7I_D7lxGJXDeTiYbDI"
	testJwtBadSign = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJVc2VySUQiOjEyMywiVXNlckFnZW50IjoidGVzdC1hZ2VudCIsImlzcyI6ImdhbGEiLCJzdWIiOiJhY2Nlc3MiLCJhdWQiOlsidGVzdF9hdWQiXSwiZXhwIjoyNzg5MTkxODIyLCJuYmYiOjE1MjY4ODQyMjIsImlhdCI6MTUyNjg4NDIyMiwianRpIjoianRpLTEifQ.dzkyekCTsm6e-s-TqxIUd3Mbu5qmtfBlYCKKKTMh_is"
	testJwtNoAud   = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJVc2VySUQiOjEyMywiVXNlckFnZW50IjoidGVzdC1hZ2VudCIsImlzcyI6ImdhbGEiLCJzdWIiOiJhY2Nlc3MiLCJleHAiOjI3ODkxOTE4MjIsIm5iZiI6MTUyNjg4NDIyMiwiaWF0IjoxNTI2ODg0MjIyLCJqdGkiOiJqdGktMSJ9.vOkABUbXkBMNLuzWAE1EA4URq3GdhJa25yXBFIV5des"
	testJwtNoneAlg = "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0.eyJVc2VySUQiOjEyMywiVXNlckFnZW50IjoidGVzdC1hZ2VudCIsImlzcyI6ImdhbGEiLCJzdWIiOiJhY2Nlc3MiLCJhdWQiOlsidGVzdF9hdWQiXSwiZXhwIjoyNzg5MTkxODIyLCJuYmYiOjE1MjY4ODQyMjIsImlhdCI6MTUyNjg4NDIyMiwianRpIjoianRpLTEifQ."
)

func testKeyFunc(*jwt.Token) (any, error) { return []byte(testKey), nil }

func testClaims() *CustomClaims {
	return &CustomClaims{
		UserID:    123,
		UserAgent: "test-agent",
		RegisteredClaims: jwt.RegisteredClaims{
			ID:       "jti-1",
			Issuer:   "gala",
			Subject:  "access",
			Audience: jwt.ClaimStrings{"test_aud"},
			// 时间用 Local()：jwt/v5 解析出来的是 time.Unix() 的本地时间，不然 DeepEqual 比不过
			IssuedAt:  jwt.NewNumericDate(time.Date(2018, 5, 21, 6, 30, 22, 0, time.UTC).Local()),
			NotBefore: jwt.NewNumericDate(time.Date(2018, 5, 21, 6, 30, 22, 0, time.UTC).Local()),
			ExpiresAt: jwt.NewNumericDate(time.Date(2058, 5, 21, 7, 30, 22, 0, time.UTC).Local()),
		},
	}
}

// testClaimsNoAud 同 testClaims 但没有 aud，用来证明 jwtx 默认不校验 aud
func testClaimsNoAud() *CustomClaims {
	c := testClaims()
	c.Audience = nil
	return c
}

func TestGenerateToken(t *testing.T) {
	tProvider := func(*jwt.Token) (any, error) {
		return []byte(testKey), nil
	}
	// 两边共用同一个 exp，签出的 token 串才能逐个字节相等
	exp := jwt.NewNumericDate(time.Now().Add(time.Hour))
	withExp := func() jwt.Claims {
		return &CustomClaims{RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: exp}}
	}
	testCases := []struct {
		// 名字
		name string

		// 要提前准备数据
		before func(t *testing.T)
		// 验证并且删除数据
		after func(t *testing.T)

		// 预期入参
		token         func() string
		tokenProvider jwt.Keyfunc
		signingMethod jwt.SigningMethod
		claims        func() jwt.Claims
		key           string

		// 预期响应
		want    any
		wantErr error
	}{
		{
			name:   "成功生成token",
			before: func(t *testing.T) {},
			after:  func(t *testing.T) {},
			token: func() string {
				tokenStr, err := jwt.
					NewWithClaims(jwt.SigningMethodHS256, withExp()).
					SignedString([]byte(testKey))
				assert.NoError(t, err)
				return tokenStr
			},
			tokenProvider: tProvider,
			signingMethod: jwt.SigningMethodHS256,
			claims:        withExp,
			wantErr:       nil,
			key:           testKey,
		},
		{
			name:   "CustomClaims 为 nil",
			before: func(t *testing.T) {},
			after:  func(t *testing.T) {},
			token: func() string {
				return "" // claims 为 nil 不再签发
			},
			tokenProvider: tProvider,
			signingMethod: jwt.SigningMethodHS256,
			claims: func() jwt.Claims {
				return nil
			},
			wantErr: ErrMissingClaims,
			key:     testKey,
		},
		{
			name:   "claims 没有 exp",
			before: func(t *testing.T) {},
			after:  func(t *testing.T) {},
			token: func() string {
				return "" // 没有 exp 不再签发，否则就是永不过期的 token
			},
			tokenProvider: tProvider,
			signingMethod: jwt.SigningMethodHS256,
			claims: func() jwt.Claims {
				return &CustomClaims{UserID: 123}
			},
			wantErr: ErrNoExpiration,
			key:     testKey,
		},
		{
			name:   "miss token provider",
			before: func(t *testing.T) {},
			after:  func(t *testing.T) {},
			token: func() string {
				return ""
			},
			tokenProvider: nil,
			signingMethod: jwt.SigningMethodHS512,
			claims:        withExp,
			wantErr:       ErrNeedTokenProvider,
			key:           testKey,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tc.before(t)
			tokenStr, err := GenerateToken(
				tc.tokenProvider,
				WithClaims(tc.claims),
				WithSigningMethod(jwt.SigningMethodHS256),
			)
			assert.ErrorIs(t, err, tc.wantErr)
			if tc.wantErr == nil {
				assert.Equal(t, tc.token(), tokenStr)
			}
			tc.after(t)
		})
	}
}

func TestParseToken(t *testing.T) {
	tProvider := func(*jwt.Token) (any, error) {
		return []byte(testKey), nil
	}
	claims := &CustomClaims{}
	claims.UserID = 123
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString([]byte(testKey))
	if err != nil {
		panic(err)
	}

	testCases := []struct {
		// 名字
		name string

		// 要提前准备数据
		before func(t *testing.T)
		// 验证并且删除数据
		after func(t *testing.T)

		// 预期入参
		tokenStr      string
		tokenProvider jwt.Keyfunc
		signingMethod jwt.SigningMethod
		claims        func() jwt.Claims
		key           string

		// 预期响应
		want    any
		wantErr error
	}{
		{
			name:     "解析token成功",
			before:   func(t *testing.T) {},
			after:    func(t *testing.T) {},
			tokenStr: tokenStr,
			claims: func() jwt.Claims {
				return &CustomClaims{}
			},
			signingMethod: jwt.SigningMethodHS256,
			tokenProvider: tProvider,
			wantErr:       nil,
		},
		{
			name:     "miss key",
			before:   func(t *testing.T) {},
			after:    func(t *testing.T) {},
			tokenStr: tokenStr,
			claims: func() jwt.Claims {
				return &CustomClaims{}
			},
			signingMethod: jwt.SigningMethodHS256,
			tokenProvider: nil,
			wantErr:       ErrMissingKeyFunc,
		},
		{
			name:     "method invalid",
			before:   func(t *testing.T) {},
			after:    func(t *testing.T) {},
			tokenStr: tokenStr,
			claims: func() jwt.Claims {
				return &CustomClaims{}
			},
			signingMethod: jwt.SigningMethodHS512,
			tokenProvider: tProvider,
			wantErr:       ErrUnSupportSigningMethod,
		},
		{
			name:     "token invalid",
			before:   func(t *testing.T) {},
			after:    func(t *testing.T) {},
			tokenStr: "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJVc2VySUQiOjAsIlVzZXJBZ2VudCI6IiJ9.xM8P27ckU6C3TxlW2fwPlFrr4P2ROE2hBoT3Gbls",
			claims: func() jwt.Claims {
				return &CustomClaims{}
			},
			signingMethod: jwt.SigningMethodHS512,
			tokenProvider: tProvider,
			wantErr:       ErrTokenInvalid,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tc.before(t)
			token, err := ParseToken(
				tc.tokenStr,
				tc.tokenProvider,
				WithClaims(tc.claims),
				WithSigningMethod(tc.signingMethod),
			)

			if err == nil {
				if c, ok := token.Claims.(*CustomClaims); ok && token.Valid {
					slog.Info("Claims:", slog.Any("claims", c))
				}
			}

			assert.ErrorIs(t, err, tc.wantErr)

			tc.after(t)
		})
	}
}

func TestParseTokenGuards(t *testing.T) {
	// 算法不符的 token 不进入调用方的 keyFunc
	called := false
	_, err := ParseToken(testJwtValid, func(*jwt.Token) (any, error) {
		called = true
		return []byte(testKey), nil
	}, WithSigningMethod(jwt.SigningMethodHS512), WithClaims(func() jwt.Claims { return &CustomClaims{} }))
	assert.ErrorIs(t, err, ErrUnSupportSigningMethod)
	assert.False(t, called, "keyFunc 不该被非预期算法的 token 调到")

	// 透传 parser option：过期 token 也解析得出来（签名照验，只是跳过时间校验），供刷新流程使用
	tok, err := ParseToken(testJwtExpired, testKeyFunc,
		WithClaims(func() jwt.Claims { return &CustomClaims{} }),
		WithParserOptions(jwt.WithoutClaimsValidation()))
	require.NoError(t, err)
	require.NotNil(t, tok)
	assert.True(t, tok.Valid)
}

func TestValidateToken(t *testing.T) {
	tProvider := func(*jwt.Token) (any, error) {
		return []byte(testKey), nil
	}
	now := time.Now()

	testCases := []struct {
		// 名字
		name string

		// 要提前准备数据
		before func(t *testing.T)
		// 验证并且删除数据
		after func(t *testing.T)

		// 预期入参
		token         func() string
		tokenProvider jwt.Keyfunc
		signingMethod jwt.SigningMethod
		claims        func() jwt.Claims
		key           string

		// 预期响应
		want    any
		wantErr error
	}{
		{
			name:   "成功生成并验证 access token",
			before: func(t *testing.T) {},
			after:  func(t *testing.T) {},
			token: func() string {
				tokenStr, err := jwt.
					NewWithClaims(jwt.SigningMethodHS256, &CustomClaims{
						UserID:    123,
						UserAgent: "test-agent",
						RegisteredClaims: jwt.RegisteredClaims{
							Subject:   "access",
							ExpiresAt: jwt.NewNumericDate(now.Add(2 * time.Hour)),
							IssuedAt:  jwt.NewNumericDate(now),
							NotBefore: jwt.NewNumericDate(now),
						},
					}).
					SignedString([]byte(testKey))
				assert.NoError(t, err)
				return tokenStr
			},
			tokenProvider: tProvider,
			signingMethod: jwt.SigningMethodHS256,
			claims: func() jwt.Claims {
				return &CustomClaims{
					UserID:    123,
					UserAgent: "test-agent",
					RegisteredClaims: jwt.RegisteredClaims{
						Subject:   "access",
						ExpiresAt: jwt.NewNumericDate(now.Add(2 * time.Hour)),
						IssuedAt:  jwt.NewNumericDate(now),
						NotBefore: jwt.NewNumericDate(now),
					},
				}
			},
			want:    "access",
			wantErr: nil,
			key:     testKey,
		},
		{
			name:   "成功生成并验证 refresh token",
			before: func(t *testing.T) {},
			after:  func(t *testing.T) {},
			token: func() string {
				tokenStr, err := jwt.
					NewWithClaims(jwt.SigningMethodHS256, &CustomClaims{
						UserID:    123,
						UserAgent: "test-agent",
						RegisteredClaims: jwt.RegisteredClaims{
							Subject:   "refresh",
							ExpiresAt: jwt.NewNumericDate(now.Add(7 * 24 * time.Hour)),
							IssuedAt:  jwt.NewNumericDate(now),
							NotBefore: jwt.NewNumericDate(now),
						},
					}).
					SignedString([]byte(testKey))
				assert.NoError(t, err)
				return tokenStr
			},
			tokenProvider: tProvider,
			signingMethod: jwt.SigningMethodHS256,
			claims: func() jwt.Claims {
				return &CustomClaims{
					UserID:    123,
					UserAgent: "test-agent",
					RegisteredClaims: jwt.RegisteredClaims{
						Subject:   "refresh",
						ExpiresAt: jwt.NewNumericDate(now.Add(7 * 24 * time.Hour)),
						IssuedAt:  jwt.NewNumericDate(now),
						NotBefore: jwt.NewNumericDate(now),
					},
				}
			},
			want:    "refresh",
			wantErr: nil,
			key:     testKey,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tc.before(t)
			tokenStr, err := GenerateToken(
				tc.tokenProvider,
				WithClaims(tc.claims),
				WithSigningMethod(tc.signingMethod),
			)
			assert.ErrorIs(t, err, tc.wantErr)
			assert.Equal(t, tc.token(), tokenStr)
			if err != nil {
				return
			}

			token, err := ParseToken(
				tokenStr,
				tc.tokenProvider,
				WithClaims(func() jwt.Claims {
					return &CustomClaims{}
				}),
				WithSigningMethod(tc.signingMethod),
			)
			assert.ErrorIs(t, err, tc.wantErr)
			if err != nil {
				return
			}

			claims, ok := token.Claims.(*CustomClaims)
			assert.True(t, ok)
			assert.Equal(t, uint64(123), claims.UserID)
			assert.Equal(t, "test-agent", claims.UserAgent)
			assert.Equal(t, tc.want, claims.Subject)
			assert.True(t, token.Valid)
			tc.after(t)
		})
	}
}

// TestParseTokenCorpus 拿固定语料喂 ParseToken：成功的要 claims 全等，失败的要能从
// errors.Is 链上认出 jwt/v5 的具体原因，而不是只有一句 ErrTokenInvalid。
func TestParseTokenCorpus(t *testing.T) {
	testCases := []struct {
		name     string
		tokenStr string
		want     *CustomClaims // 非 nil 表示应当解析成功且 claims 逐个字段相等
		wantErr  []error       // 全部都要命中 errors.Is
	}{
		{name: "有效 token", tokenStr: testJwtValid, want: testClaims()},
		{name: "没有 aud 默认放行", tokenStr: testJwtNoAud, want: testClaimsNoAud()},
		{name: "过期", tokenStr: testJwtExpired, wantErr: []error{ErrTokenInvalid, jwt.ErrTokenExpired}},
		{name: "nbf 在未来", tokenStr: testJwtNbf, wantErr: []error{ErrTokenInvalid, jwt.ErrTokenNotValidYet}},
		{name: "签名不对", tokenStr: testJwtBadSign, wantErr: []error{ErrTokenInvalid, jwt.ErrTokenSignatureInvalid}},
		{name: "alg=none 攻击", tokenStr: testJwtNoneAlg, wantErr: []error{ErrUnSupportSigningMethod, jwt.ErrTokenUnverifiable}},
		{name: "空串", tokenStr: "", wantErr: []error{ErrTokenInvalid, jwt.ErrTokenMalformed}},
		{name: "段数不对", tokenStr: "bad.bad.bad", wantErr: []error{ErrTokenInvalid, jwt.ErrTokenMalformed}},
		{name: "根本不是 token", tokenStr: "bad", wantErr: []error{ErrTokenInvalid, jwt.ErrTokenMalformed}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tok, err := ParseToken(tc.tokenStr, testKeyFunc, WithClaims(func() jwt.Claims { return &CustomClaims{} }))

			if tc.want != nil {
				require.NoError(t, err)
				require.NotNil(t, tok)
				assert.Equal(t, tc.want, tok.Claims, "round-trip 回来的 claims 必须逐个字段相等")
				assert.True(t, tok.Valid)
				return
			}

			for _, want := range tc.wantErr {
				assert.ErrorIs(t, err, want)
			}
			assert.Nil(t, tok, "解析失败不能漏出半成品 token")
		})
	}
}

// TestParseTokenSigningMethod 换算法必须双向成立：HS512 签的要能用 HS512 解析，
// 否则「卡算法」就成了「一律拒绝」，而语料里的 HS256 用例察觉不到。
func TestParseTokenSigningMethod(t *testing.T) {
	tokenStr, err := GenerateToken(testKeyFunc,
		WithSigningMethod(jwt.SigningMethodHS512), WithClaims(func() jwt.Claims { return testClaims() }))
	require.NoError(t, err)

	tok, err := ParseToken(tokenStr, testKeyFunc,
		WithSigningMethod(jwt.SigningMethodHS512), WithClaims(func() jwt.Claims { return &CustomClaims{} }))
	require.NoError(t, err)
	assert.Equal(t, testClaims(), tok.Claims)

	// 默认 HS256 解析同一个 token 必须被拒
	_, err = ParseToken(tokenStr, testKeyFunc, WithClaims(func() jwt.Claims { return &CustomClaims{} }))
	assert.ErrorIs(t, err, ErrUnSupportSigningMethod)
}

// TestParseTokenParserOptions 同一个 token，配置不同结论就该不同；顺带证明选项
// 不跨调用泄漏（每次 ParseToken 都是新 Options）。
func TestParseTokenParserOptions(t *testing.T) {
	parse := func(tokenStr string, opts ...Option) error {
		_, err := ParseToken(tokenStr, testKeyFunc,
			append(opts, WithClaims(func() jwt.Claims { return &CustomClaims{} }))...)
		return err
	}

	assert.NoError(t, parse(testJwtValid, WithParserOptions(jwt.WithAudience("test_aud"))))
	assert.ErrorIs(t, parse(testJwtValid, WithParserOptions(jwt.WithAudience("other_aud"))), jwt.ErrTokenInvalidAudience)
	assert.NoError(t, parse(testJwtValid, WithParserOptions(jwt.WithIssuer("gala"))))
	assert.ErrorIs(t, parse(testJwtValid, WithParserOptions(jwt.WithIssuer("other"))), jwt.ErrTokenInvalidIssuer)

	// 上面刚带过 WithAudience，这里不带必须恢复宽松，否则就是选项泄漏到了下一次调用
	assert.NoError(t, parse(testJwtNoAud))
	assert.NoError(t, parse(testJwtValid))
}

// 没配 WithClaims 时 o.claims 是 nil，调它就是 nil func → panic。锁住现状：
// 想让它返回 ErrMissingClaims 而不是 panic，就在 GenerateToken 里补 o.claims == nil 守卫。
func TestGenerateTokenWithoutClaimsPanics(t *testing.T) {
	assert.Panics(t, func() { _, _ = GenerateToken(testKeyFunc) })
}
