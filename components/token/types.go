package token

import (
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims 是默认的 JWT 顶层载荷：data 放业务数据，其余用标准声明。
// 需要自定义顶层字段时用 JwtManager.GenerateClaims / ParseClaims。
type Claims[T any] struct {
	Data T `json:"data"`

	jwt.RegisteredClaims
}

// JwtConfig 是 JwtManager 的配置。
type JwtConfig struct {
	SecretKey     string             `mapstructure:"secret" json:"-"`
	Issuer        string             `mapstructure:"issuer" json:"issuer"`
	Audience      []string           `mapstructure:"audience" json:"audience"`
	Algorithm     string             `mapstructure:"algorithm" json:"algorithm"`
	Expiry        time.Duration      `mapstructure:"access_ttl" json:"access_ttl"`
	ParserOptions []jwt.ParserOption `mapstructure:"-" json:"-"`

	// Cookie* 与 SameSite 只被 Set/Get/Reset 用，纯 API 客户端（Authorization 头）不需要配。
	CookieName   string        `mapstructure:"cookie_name" json:"cookie_name"`
	CookieDomain string        `mapstructure:"cookie_domain" json:"cookie_domain"`
	CookieSecure bool          `mapstructure:"cookie_secure" json:"cookie_secure"`
	DisableXSRF  bool          `mapstructure:"disable_xsrf" json:"disable_xsrf"`
	SameSite     http.SameSite `mapstructure:"same_site" json:"same_site"`

	// TokenHeader 附加到 JWT header（如 kid）。值可以是任意类型且常由运行时决定，
	// 所以不进配置文件
	TokenHeader map[string]any `mapstructure:"-" json:"-"`
}

type JwtManager[T any] struct {
	conf *JwtConfig
}

// TokenStyle 决定 TokenGenerator 用哪种形态生成 token。
type TokenStyle string

const (
	TokenStyleSimple    TokenStyle = "simple"
	TokenStyleTimestamp TokenStyle = "timestamp"
	TokenStyleUUID      TokenStyle = "uuid"
	TokenStyleHash      TokenStyle = "hash"
	TokenStyleJWT       TokenStyle = "jwt"
	TokenStyleTiktok    TokenStyle = "tiktok"
)

// TokenConfig 是 TokenGenerator 的配置，JWT* 字段透传给 JwtConfig。
type TokenConfig struct {
	JWTIssuer    string        `json:"jwt_issuer"`
	JWTSecretKey string        `json:"jwt_secret_key"`
	JWTAudience  []string      `json:"jwt_audience" `
	JWTAlgorithm string        `json:"jwt_algorithm"`
	JWTExpiry    time.Duration `json:"jwt_expiry" `

	TokenStyle TokenStyle `json:"token_style"`

	SimpleTokenLength int `json:"simple_token_length"`
}

type Generator[T any] interface {
	GenerateToken(userID string, data T) (string, error)
}

type TokenGenerator[T any] struct {
	config *TokenConfig
}
