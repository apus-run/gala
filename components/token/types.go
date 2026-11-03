// Package token issues and verifies tokens: a style-pluggable TokenGenerator
// and an HTTP-oriented JwtManager | 令牌签发与校验：多风格 TokenGenerator 与面向 HTTP 的 JwtManager.
//
// # Notes | 说明
//
// The current design is stateless: JwtManager signs HS256/384/512 JWTs with a
// shared secret (asymmetric algorithms are rejected early) and ParseClaims
// checks signature, algorithm, and the configured issuer/audience. There is
// no server-side session store, so tokens cannot be revoked — Reset only
// clears the cookies and a leaked token stays valid until expiry. Set
// delivers an HttpOnly token cookie plus a readable XSRF cookie (double-
// submit, checked by Get on unsafe methods) and returns the raw token for
// API clients. Of the TokenGenerator styles only "jwt" carries data and can
// be verified; the others return opaque random strings.
// 当前为无状态设计：JwtManager 用共享密钥签 HS256/384/512 JWT（非对称算法直接
// 报错），ParseClaims 校验签名、算法及配置的 issuer/audience。没有服务端会话
// 存储，令牌不可吊销 —— Reset 只清 cookie，泄露的令牌在过期前始终有效。Set 同时
// 投递 HttpOnly token cookie、可读的 XSRF cookie（双提交校验，Get 对非安全方法
// 检查）并返回给 API 客户端的原始串。TokenGenerator 各风格中只有 "jwt" 携带
// 数据且可校验，其余返回不透明随机串。
//
// # Features | 功能
//
// - JwtManager issues, parses and verifies typed Claims[T] | JwtManager 签发/解析/校验泛型 Claims[T]
// - HTTP helpers Set / Get / Reset with XSRF double-submit | Set / Get / Reset 与 XSRF 双提交
// - TokenGenerator: simple / uuid / timestamp / hash / tiktok / jwt | 六种风格的 TokenGenerator
// - Hash gives a SHA-256 hex digest for storage | Hash 生成 SHA-256 摘要用于存储比对
//
// # Example | 示例
//
//	jm := token.NewJwtManager[UserInfo](token.WithSecretKey(secret))
//	claims, tok, err := jm.Set(w, "10001", UserInfo{Role: "admin"})
//	if err != nil {
//		return err
//	}
//	_ = tok // for the JSON body or Authorization header
//	claims, _, err = jm.Get(r)
package token

import (
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims is the default JWT top-level payload: Data carries the business data,
// the rest are standard registered claims.
// Claims 是默认的 JWT 顶层载荷：data 放业务数据，其余用标准声明。
// Use JwtManager.GenerateClaims / ParseClaims to sign or parse custom top-level payloads.
// 需要自定义顶层字段时用 JwtManager.GenerateClaims / ParseClaims。
type Claims[T any] struct {
	// Data carries the business payload | data 存放业务数据.
	Data T `json:"data"`

	jwt.RegisteredClaims
}

// JwtConfig configures JwtManager | JwtConfig 是 JwtManager 的配置.
type JwtConfig struct {
	// SecretKey is the shared HMAC secret | 共享 HMAC 密钥.
	SecretKey string `mapstructure:"secret" json:"-"`
	// Issuer is written into the claims | 签发者（写入 claims）.
	Issuer string `mapstructure:"issuer" json:"issuer"`
	// Audience is the expected audience | 受众.
	Audience []string `mapstructure:"audience" json:"audience"`
	// Algorithm is the signing algorithm, HS* only | 签名算法，仅支持 HS*.
	Algorithm string `mapstructure:"algorithm" json:"algorithm"`
	// Expiry is the token lifetime | 令牌有效期.
	Expiry time.Duration `mapstructure:"access_ttl" json:"access_ttl"`
	// ParserOptions are shared by all parse calls | 所有解析调用共用的解析选项.
	ParserOptions []jwt.ParserOption `mapstructure:"-" json:"-"`

	// Cookie* and SameSite are only used by Set/Get/Reset; pure API clients (Authorization header) need none of them.
	// Cookie* 与 SameSite 只被 Set/Get/Reset 用，纯 API 客户端（Authorization 头）不需要配。
	CookieName   string        `mapstructure:"cookie_name" json:"cookie_name"`
	CookieDomain string        `mapstructure:"cookie_domain" json:"cookie_domain"`
	CookieSecure bool          `mapstructure:"cookie_secure" json:"cookie_secure"`
	DisableXSRF  bool          `mapstructure:"disable_xsrf" json:"disable_xsrf"`
	SameSite     http.SameSite `mapstructure:"same_site" json:"same_site"`

	// TokenHeader is added to the JWT header (e.g. kid); values can be any type and are
	// often decided at runtime, so they are not loaded from config files.
	// TokenHeader 附加到 JWT header（如 kid）。值可以是任意类型且常由运行时决定，
	// 所以不进配置文件
	TokenHeader map[string]any `mapstructure:"-" json:"-"`
}

// JwtManager signs and verifies typed JWT claims over HTTP | JwtManager 面向 HTTP 签发与校验泛型 JWT claims.
type JwtManager[T any] struct {
	conf *JwtConfig
}

// TokenStyle decides which shape TokenGenerator uses to mint tokens | TokenStyle 决定 TokenGenerator 用哪种形态生成 token.
type TokenStyle string

const (
	// TokenStyleSimple is an opaque random string | 不透明随机串.
	TokenStyleSimple TokenStyle = "simple"
	// TokenStyleTimestamp is "unix_milli_userID_random" | “毫秒时间戳_用户_随机串”.
	TokenStyleTimestamp TokenStyle = "timestamp"
	// TokenStyleUUID is a UUIDv7 | UUIDv7 标识.
	TokenStyleUUID TokenStyle = "uuid"
	// TokenStyleHash is a SHA-256 hex digest | SHA-256 十六进制摘要.
	TokenStyleHash TokenStyle = "hash"
	// TokenStyleJWT carries data and is verifiable | 携带数据且可校验.
	TokenStyleJWT TokenStyle = "jwt"
	// TokenStyleTiktok is an 11-char alphanumeric string | 11 位字母数字串.
	TokenStyleTiktok TokenStyle = "tiktok"
)

// TokenConfig configures TokenGenerator; the JWT* fields are forwarded to JwtConfig | TokenConfig 是 TokenGenerator 的配置，JWT* 字段透传给 JwtConfig.
type TokenConfig struct {
	// JWT* fields are forwarded to JwtConfig | JWT* 字段透传给 JwtConfig.
	JWTIssuer    string        `json:"jwt_issuer"`
	JWTSecretKey string        `json:"jwt_secret_key"`
	JWTAudience  []string      `json:"jwt_audience" `
	JWTAlgorithm string        `json:"jwt_algorithm"`
	JWTExpiry    time.Duration `json:"jwt_expiry" `

	// TokenStyle selects the generator style | 生成风格.
	TokenStyle TokenStyle `json:"token_style"`

	// SimpleTokenLength is the length of simple tokens | simple 风格的令牌长度.
	SimpleTokenLength int `json:"simple_token_length"`
}

// Generator mints tokens for users | Generator 为用户签发令牌的接口.
type Generator[T any] interface {
	// GenerateToken mints a token for userID carrying data | GenerateToken 为 userID 签发携带 data 的令牌.
	GenerateToken(userID string, data T) (string, error)
}

// TokenGenerator is the style-pluggable token minter | TokenGenerator 是可切换风格的令牌生成器.
type TokenGenerator[T any] struct {
	config *TokenConfig
}
