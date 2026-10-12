# token

token 生成与校验：6 种 token 形态（JWT、UUID、纯随机串等）由 `TokenGenerator` 统一分派，JWT 场景用 `JwtManager` 管配置、声明与投递（浏览器 cookie / API 客户端 `Authorization` 头）。JWT 部分建在 [`pkg/jwtx`](github.com/apus-run/gala/pkg/jwtx) 之上，只支持 HS256/384/512 共享密钥，没有内置默认密钥。

## 安装

要求 **Go 1.27 或更高版本**。

```sh
go get github.com/apus-run/gala/components/token
```

以下接口与示例对应当前仓库源码。使用尚未发布的变更时，在应用的 `go.mod` 中通过 `replace` 指向本地 `components/token` 模块。当前模块自身对 `pkg/jwtx` 也是本地 `replace`（新代码尚未打 tag）——**发布前需要给 `pkg/jwtx` 打 tag，然后删掉那行 replace 并把 require 提到该版本**，否则使用者拿到的是旧行为（例如旧版 jwtx 不拦没有 exp 的 claims）。

## 接口签名

```go
// JWT 管理器：配置 + 签发 + 校验
func NewJwtManager[T any](opts ...option.Option[JwtConfig]) *JwtManager[T]

func (jm *JwtManager[T]) GenerateToken(userID string, data T) (string, error)
func (jm *JwtManager[T]) GenerateClaims(claims jwt.Claims) (string, error)
func (jm *JwtManager[T]) NewRegisteredClaims(userID string, now time.Time) (jwt.RegisteredClaims, error)
func (jm *JwtManager[T]) ParseToken(tokenStr string) (*Claims[T], error)
func (jm *JwtManager[T]) ParseClaims(tokenStr string, claims jwt.Claims, opts ...jwt.ParserOption) error
func (jm *JwtManager[T]) VerifyToken(tokenStr string) (bool, error)

// 投递层：浏览器用 cookie，API 客户端用 Authorization 头
func (jm *JwtManager[T]) Set(w http.ResponseWriter, userID string, data T, opts ...SetOption) (*Claims[T], string, error)
func (jm *JwtManager[T]) Get(r *http.Request) (*Claims[T], string, error)
func (jm *JwtManager[T]) Reset(w http.ResponseWriter)

type SetOption func(*setOptions)
func WithSessionCookie() SetOption // 这次只写会话级 cookie

const (
	XSRFCookieName = "XSRF-TOKEN" // 双提交用的 XSRF cookie 名
	XSRFHeaderKey  = "X-XSRF-TOKEN"
)

// 多形态生成器
func NewTokenGenerator[T any](opts ...option.Option[TokenConfig]) *TokenGenerator[T]

type Generator[T any] interface {
	GenerateToken(userID string, data T) (string, error)
}

// 辅助
func GenerateJWTSecret(length int) string // hex 字符串，长度为 2*length；length <= 0 按 32 处理
func Hash(plain string) string            // SHA-256 hex，供存储/查找 token 摘要

func NewJwtConfig(opts ...option.Option[JwtConfig]) *JwtConfig
func DefaultJwtConfig() *JwtConfig
func NewTokenConfig(opts ...option.Option[TokenConfig]) *TokenConfig
func DefaultTokenConfig() TokenConfig
```

`Claims[T]` 是默认的顶层载荷（`Data T` + `jwt.RegisteredClaims`）。`*JwtManager[T]` 和 `*TokenGenerator[T]` 都满足 `Generator[T]`。

## TokenStyle

`TokenGenerator` 按 `TokenStyle` 分派，除 `jwt` 外都不需要密钥：

| Style | 产物 | 说明 |
| --- | --- | --- |
| `TokenStyleJWT`（默认） | JWT | 转交 `JwtManager`，配置见下表 |
| `TokenStyleSimple` | 62 进制随机串 | 长度取 `SimpleTokenLength` |
| `TokenStyleUUID` | UUIDv7 | 时间有序（stdlib `uuid`） |
| `TokenStyleTimestamp` | `毫秒时间戳_userID_16位hex` | **userID 明文可见**；随机部分 8 字节 |
| `TokenStyleHash` | SHA-256 hex | 摘取自 `userID:纳秒时间:16 字节随机` |
| `TokenStyleTiktok` | 11 位 62 进制串 | 长度固定 `TiktokTokenLength` |

## Option

`JwtManager` 的选项（`option.Option[JwtConfig]`）：

| 选项 | 说明 | 默认值 |
| --- | --- | --- |
| `WithSecretKey(string)` | HS* 共享密钥 | 空 → 签发/校验返回 `ErrEmptyJWTSecret` |
| `WithIssuer(string)` | 写入 `iss`；**解析时按它校验**（要关掉：单次调用传 `jwt.WithIssuer("")`） | `gala/token` |
| `WithAudience(...string)` | `aud` 白名单；**解析时按它校验**（必须存在且命中其一） | 空（不校验） |
| `WithAlgorithm(string)` | `HS256`/`HS384`/`HS512` | `HS256` |
| `WithExpiry(time.Duration)` | `GenerateToken` 写进 claims 的有效期 | 7 天 |
| `WithParserOptions(...jwt.ParserOption)` | 所有解析调用共用的校验规则 | 无 |
| `WithTokenHeader(map[string]any)` | 附加到 JWT header 的自定义字段（如 `kid`）；签发时生效 | 无 |
| `WithCookieName(string)` | cookie 名（只被 `Set`/`Get`/`Reset` 用） | `"token"` |
| `WithCookieDomain(string)` | cookie 的 Domain | 空（只对当前 host 生效） |
| `WithCookieSecure(bool)` | cookie 的 Secure | `true`（本地 http 调试要显式关掉） |
| `WithSameSite(http.SameSite)` | cookie 的 SameSite，用标准库的类型 | `http.SameSiteLaxMode` |
| `WithDisableXSRF(bool)` | 关掉 cookie 请求的 XSRF 校验 | `false` |
| `WithJwtConfig(JwtConfig)` | 用整份配置覆盖，空字段回落到默认值 | — |

`TokenGenerator` 的选项（`option.Option[TokenConfig]`）：`WithTokenStyle`、`WithSimpleTokenLength`、以及 `JWT*` 前缀的一组（`WithJWTSecretKey`/`WithJWTIssuer`/`WithJWTAudience`/`WithJWTAlgorithm`/`WithJWTExpiry`，透传给 `JwtManager`）、`WithTokenConfig`。

`WithSimpleTokenLength` 对应 `SimpleTokenLength`，传非正数按 `DefaultSimpleLength`（16）处理。

## 错误

| 错误 | 触发 |
| --- | --- |
| `ErrEmptyUserID` | userID 为空或只有空白 |
| `ErrEmptyToken` | 解析空串 |
| `ErrEmptyJWTSecret` | 没配密钥就签发/校验 |
| `ErrUnsupportedAlgorithm` | 算法不是 HS256/384/512 |
| `ErrInvalidToken` | claims 参数为 nil |
| `ErrUnsupportedTokenStyle` | `TokenStyle` 不认识 |
| `ErrGenerateToken` | 随机源失败 |
| `ErrMissingToken` | 请求里既没有 `Authorization` 头也没有 cookie |
| `ErrXSRFMismatch` | cookie 来源的请求，`X-XSRF-TOKEN` 与 claims 的 `jti` 不一致 |

解析失败的错误链上还能认出 jwt/v5 的原因：`errors.Is(err, jwt.ErrTokenExpired)`、`jwt.ErrTokenInvalidIssuer`、`jwt.ErrTokenInvalidAudience` 等。

## 快速开始

```go
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
```

输出（完整可运行版本见 `example_test.go`，由测试保证与实现一致）：

```text
sign: true
claims: user-1 s-1 <nil>
cross-service: true true
no secret: true
```

非 JWT 形态：

```go
gen := token.NewTokenGenerator[Payload](
	token.WithTokenStyle(token.TokenStyleSimple),
	token.WithSimpleTokenLength(24),
)
raw, err := gen.GenerateToken("user-1", Payload{})
fmt.Println("simple:", len(raw), err)
```

```text
simple: 24 <nil>
```

## 浏览器与 API 客户端

两条投递路径共用同一个签名 token，变的是投递方式和威胁模型：

| | API 客户端 | 浏览器 |
| --- | --- | --- |
| 凭据 | `Authorization: Bearer <token>` | cookie |
| CSRF 面 | 无（不是环境凭据） | **有**，所以必须过 XSRF 双提交 |
| 取 token | `Get(r)` 优先读头 | `Get(r)` 回落到 cookie |

```go
// 登录：签发 + 投递。浏览器拿走 cookie，API 客户端拿返回的 token 串放进 JSON body
claims, raw, err := manager.Set(w, "user-1", Payload{SessionID: "s-1"})

// 每个请求：头或 cookie 都认
claims, raw, err := manager.Get(r)

// 登出
manager.Reset(w)
```

- `Set` 写两个 cookie：token 的 `HttpOnly`（JS 读不到，防 XSS 偷 token），XSRF 的**故意不设** `HttpOnly`。两者属性都取自配置。签发失败（如 userID 为空）时**不写任何 cookie**。
- **cookie 寿命就是 token 寿命**：`Max-Age` 取 `Expiry`。组件不做刷新（`Get` 走完整校验、过期即拒），所以不存在"cookie 活得比 token 长"的用法。
- **会话级 cookie**：`Set(w, userID, data, token.WithSessionCookie())` 不写 `Max-Age`/`Expires`，浏览器关掉即登出。它是"这次登录"的决定（"记住我"勾没勾），所以是调用参数而不是配置；只影响浏览器存多久，token 自身的 `exp` 不变。
- **`SameSite` 直接收 `http.SameSite`**，不另造一套枚举。配 `SameSiteNoneMode` 时强制带 `Secure`（浏览器会直接丢掉没有 `Secure` 的 `SameSite=None` cookie，不强制就是"配了却没生效"）。
- `Get` 的 XSRF 规则：只有**来自 cookie** 且方法不是 `GET`/`HEAD`/`OPTIONS` 时才要求 `X-XSRF-TOKEN` 等于 claims 的 `jti`（`jti` 为空时一律拒 —— 否则头缺失与空 `jti` 会"相等"，校验形同虚设）—— 即双提交。头来源的请求不需要，因为 `Authorization` 不是浏览器自动带上的凭据。别处已有 CSRF 防护时用 `WithDisableXSRF(true)` 关掉。
- 过期就是过期：`Get` 走完整校验（含 exp），没有"过期仍放行交给中间件刷新"的模式 —— 要刷新就用 `Set` 重新签一个。
- `CookieSecure` 默认 `true`。**本地 http 调试必须 `WithCookieSecure(false)`**，否则浏览器不存这个 cookie，表现为"登录成功但下一个请求 401"。

## JWT 的校验规则

- **签发**：`GenerateToken` 用配置填 `exp`/`iat`/`nbf`/`iss`/`aud`/`sub`，`jti` 取 UUIDv7；`GenerateClaims` 则完全用调用方给的 claims，**必须自带 exp**，否则 `pkg/jwtx` 的闸门返回 `ErrNoExpiration`。
- **校验**：配置里声明过的 `Issuer`/`Audience` 由验证方校验 —— 签发时写进 claims 不算数，否则另一个服务用同一个密钥签的 token 也能过。`Issuer` 有默认值所以默认就在校验，单次调用传 `jwt.WithIssuer("")` 可关掉（jwt/v5 对空的期望值不校验）；`Audience` 不配就不校验。`ParseClaims` 的 `opts` 追加在配置之后，可以覆盖（例如按调用传 `jwt.WithLeeway`、`jwt.WithTimeFunc`）。
- **算法**：只认 HS256/384/512。配 `RS256`/`ES256` 之类会返回 `ErrUnsupportedAlgorithm`，而不是静默换算法；解析时算法在进 keyFunc 之前就卡住。
- **密钥**：没有默认密钥，`DefaultJwtConfig()` 的 `SecretKey` 是空串。忘了配就是签发失败，不会用一个人人皆知的值签出 token。

---

`JwtConfig` 的字段带 `mapstructure` tag，可直接接 `components/conf` 之类的配置加载；`TokenConfig` 带 `json` tag。`SecretKey` / `ParserOptions` 标了 `json:"-"`，不会被序列化出去。
