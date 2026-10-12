# jwtx

`jwt/v5` 的薄封装：签发、解析、验签、密钥生成，外加两个 bcrypt 口令哈希函数。claims 类型由调用方定义（如 `CustomClaims`），组件不内置，只保证两件事：**签出的 token 一定有 exp**，**解析时先按配置的算法卡住** —— 非预期 `alg` 的 token 连调用方的 keyFunc 都进不去。另有两个与 JWT 无关、只是常一起用的 bcrypt 函数。

## 安装

要求 **Go 1.27 或更高版本**。

```sh
go get github.com/apus-run/gala/pkg/jwtx
```

以下接口与示例对应当前仓库源码。使用尚未发布的变更时，在应用的 `go.mod` 中通过 `replace` 指向本地 `pkg/jwtx` 模块。

## 接口签名

```go
func GenerateToken(keyProvider jwt.Keyfunc, opts ...Option) (string, error)
func ParseToken(tokenStr string, keyFunc jwt.Keyfunc, opts ...Option) (*jwt.Token, error)

func GenerateJWTSecret(length int) []byte

func Encrypt(source string) (string, error)
func Compare(hashedPassword, password string) error

type ClaimsFunc func() jwt.Claims

func DefaultOptions() *Options
func Apply(opts ...Option) *Options
```

`keyProvider` / `keyFunc` 都是 `jwt.Keyfunc`，拿到的是已解出 header 和 claims 的 `*jwt.Token`，可以按 `token.Header["kid"]` 选密钥。`DefaultOptions` 和 `Apply` 是选项装配的内部入口，正常用不到。

## Option

| 选项 | 说明 | 默认值 |
| --- | --- | --- |
| `WithSigningMethod(jwt.SigningMethod)` | 签名算法；解析时以它为准卡 `alg` | `HS256` |
| `WithClaims(ClaimsFunc)` | claims 工厂，返回的 claims **必须自带 exp** | 无 |
| `WithTokenHeader(map[string]any)` | 附加到 token header（如 `kid`） | 无 |
| `WithParserOptions(...jwt.ParserOption)` | 透传 jwt/v5 的解析选项，如 `WithAudience`/`WithIssuer`/`WithLeeway`/`WithoutClaimsValidation` | 无 |

选项按传入顺序应用，每次调用 `GenerateToken`/`ParseToken` 都会新建一份 Options，**不会跨调用泄漏**。

## 错误

失败时都返回这些哨兵错误，底层原因用 `%w` 挂在链上，所以既可以用 `errors.Is` 认自己的错误，也能认出 jwt/v5 的原因。

| 错误 | 触发 |
| --- | --- |
| `ErrNeedTokenProvider` | `GenerateToken` 的 keyProvider 为 nil |
| `ErrMissingKeyFunc` | `ParseToken` 的 keyFunc 为 nil |
| `ErrMissingClaims` | `WithClaims` 的工厂返回了 nil |
| `ErrNoExpiration` | claims 没有 exp（或取 exp 报错）—— 拒绝签发 |
| `ErrUnSupportSigningMethod` | token 的 `alg` 与 `WithSigningMethod` 不一致 |
| `ErrTokenInvalid` | 解析失败的总错误（签名错、过期、nbf、段数不对……） |
| `ErrGetKey` | 调用方 keyFunc 返回错误 |
| `ErrSignToken` | `SignedString` 失败 |

## 快速开始

```go
package main

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

func main() {
	claims := &Claims{
		UserID:           42,
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))},
	}

	tokenStr, err := jwtx.GenerateToken(keyFunc, jwtx.WithClaims(func() jwt.Claims { return claims }))
	fmt.Println("sign:", err == nil)

	tok, err := jwtx.ParseToken(tokenStr, keyFunc, jwtx.WithClaims(func() jwt.Claims { return &Claims{} }))
	fmt.Println("uid:", tok.Claims.(*Claims).UserID, err)

	// 过期的 token 照样返回 ErrTokenInvalid，但原因可以从错误链上认出来
	expired := &Claims{UserID: 42, RegisteredClaims: jwt.RegisteredClaims{
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
	}}
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
}
```

输出（这段代码就是 `example_test.go`，由测试保证与实现一致）：

```text
sign: true
uid: 42 <nil>
expired: true true
no exp: true
alg: true
```

## 两条不变量

- **签发必带 exp**。jwt/v5 默认不要求 exp，漏设就等于签出永不过期的凭证，所以 `GenerateToken` 在签之前先取 `claims.GetExpirationTime()`，拿不到就返回 `ErrNoExpiration`，什么都不签。claims 由调用方定义，exp 也只能由调用方填 —— 组件不替你补，也不内置 claims 类型。
- **解析先卡算法**。`ParseToken` 在调用 keyFunc **之前**比对 `token.Method` 与 `WithSigningMethod`，不一致直接返回 `ErrUnSupportSigningMethod`。所以 `alg: none` 之类不会拿到你的密钥，keyFunc 里不必再自己做算法白名单。
- **过期与伪造是可分的**。`ParseToken` 默认拒绝过期 token（jwt/v5 的默认校验），但原因留在链上：`errors.Is(err, jwt.ErrTokenExpired)` / `jwt.ErrTokenNotValidYet` / `jwt.ErrTokenSignatureInvalid` / `jwt.ErrTokenMalformed`。需要"过期也要解析出来做刷新"时，传 `WithParserOptions(jwt.WithoutClaimsValidation())`，签名仍然照验。
- **解析选项削不弱算法校验**。`WithParserOptions` 里的 `jwt.WithValidMethods` 就算传宽松了，`WithSigningMethod` 那条比对依然是硬约束。

## 注意

- **不配 `WithClaims` 直接调用 `GenerateToken` 会 panic**（nil 函数调用）。`WithClaims` 的工厂返回 nil 则得到 `ErrMissingClaims`。要签"没有业务字段"的 token，也得给一个返回空 claims 的工厂，并自己填 exp。
- `GenerateJWTSecret` 返回 `length` 个随机字节；`crypto/rand` 报错时会退回用时间戳拼密钥（该分支在 Go 1.24+ 实际不可达 —— crypto/rand 失败即崩进程），别把它当作可用的兜底。
- 组件不做多受众密钥（`aud` → 不同 secret）、不校验 `aud`/`iss`、不涉及 cookie 与 HTTP 传输。这些交给调用方：`aud`/`iss` 用 `WithParserOptions(jwt.WithAudience(...), jwt.WithIssuer(...))`，传输层参考 `components/authn`、`components/token`。
