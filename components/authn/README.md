# authn

`JwtAuth` 保留原有单令牌签发、校验与吊销接口。`SessionAuth` 提供有状态的双令牌会话管理，JWT 签名与标准声明校验仍委托 `components/token`。

## 接入

```go
sessionStore := store.NewStore(redisClient, "myapp:auth:")
auth, err := authn.NewSessionAuth(sessionStore,
    authn.WithJWTOptions(token.WithSecretKey(secret), token.WithIssuer("myapp")),
    authn.WithSessionTTL(15*time.Minute, 7*24*time.Hour),
)
// 检查 err 后，在密码/验证码等身份认证成功时创建独立会话：
pair, err := auth.Issue(ctx, userID)
// API 中间件验证 Access Token，并以 claims.Subject 查询业务身份：
claims, err := auth.ParseAccess(ctx, accessToken)
// 刷新接口只接受 Refresh Token：
next, err := auth.Refresh(ctx, refreshToken)
// 登出撤销该登录会话；其他设备/登录会话不受影响：
err = auth.Logout(ctx, accessToken) // 也可传未过期的 Refresh Token
```

以上为调用片段，每次调用都必须检查错误。业务角色、用户禁用检查由上层完成；刷新前若需要读取身份，可先用 `components/token` 校验 Refresh Token 的签名并读取 subject，最终仍必须调用 `Refresh` 完成用途检查与原子消费。

## 请求认证中间件

`SessionAuth.AuthFunc` 从 `Authorization: Bearer <token>` 取 Access Token，并复用 `ParseAccess` 完成用途校验与会话 generation 校验。核心适配只依赖标准库：

```go
authFn := sessionAuth.AuthFunc()
handler := authn.NewMiddleware(authFn).Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    claims, ok := authn.FromContext[*authn.SessionClaims](r.Context())
    if !ok {
        http.Error(w, "identity missing", http.StatusInternalServerError)
        return
    }
    // claims.Subject 是已认证用户
}))
```

认证失败时中间件不会调用下游 handler。默认错误响应由 `StatusOf` 决定：

| 错误 | HTTP 状态 | `WWW-Authenticate` |
| --- | --- | --- |
| `authn.ErrMissingToken` | 401 | `Bearer` |
| `authn.ErrTokenInvalid`、`authn.ErrSessionInvalid` | 401 | `Bearer error="invalid_token"` |
| 其他错误（例如存储不可用） | 503 | 无 |

401 与 503 都失败关闭。`StatusOf` 使用 `errors.Is`，所以包装后的错误也能正确分类。需要自定义响应体时，可用 `authn.WithErrorHandler` 替换默认响应。

`authn.FromContext[T]` 只在认证成功后读取；类型不匹配或没有身份时返回 `(zero, false)`，不会 panic。白名单路径不会写入身份。

### 令牌来源

`AuthFunc` 接受可选的 `authn.TokenSource` 列表，按顺序尝试，用第一个取到令牌的来源。不传时只读 `Authorization: Bearer` 头：

```go
sessionAuth.AuthFunc()                                    // API 客户端：只读 Bearer 头
sessionAuth.AuthFunc(authn.TokenFromHeader,
    authn.TokenFromCookie("access"))                      // 浏览器：先头后 Cookie
sessionAuth.AuthFunc(authn.TokenFromQuery("token"))       // WebSocket 握手等无法设头的场景
```

| 来源 | 读取位置 | 注意 |
| --- | --- | --- |
| `TokenFromHeader` | `Authorization: Bearer <token>` | scheme 不区分大小写；`Basic` 等其他 scheme 视为没有令牌 |
| `TokenFromCookie(name)` | 同名 Cookie | 浏览器会在跨站请求中自动附带 Cookie，必须配合 CSRF 防护 |
| `TokenFromQuery(name)` | 同名 query 参数 | URL 会进访问日志、浏览器历史和 Referer，只用于无法设头的客户端 |

所有内置来源都会去除首尾空白，空值视为没有令牌。

**取到令牌但校验失败时直接返回错误，不会再尝试后面的来源。**来源只负责"有没有"，不负责"对不对"：携带了无效 Bearer 头的请求不能借浏览器的 Cookie 通过认证。所有来源取到的令牌都走同一条 `ParseAccess`，用途校验、注销与存储故障的处理完全一致。

`TokenSource` 就是 `func(*http.Request) (string, bool)`，自定义来源（例如 `X-Access-Token` 头）直接写函数即可。

## Gin 接入

Gin 适配位于 `components/ginx/middlewares/auth`：

```go
import ginauth "github.com/apus-run/gala/components/ginx/middlewares/auth"

router.Use(ginauth.NewBuilder(sessionAuth.AuthFunc()).
    IgnorePaths("/login").
    IgnorePrefix("/public").
    Build())
```

- `IgnorePaths` 只做路径完全匹配。
- `IgnorePrefix` 做路径段前缀匹配：`/public` 放行 `/public/a`，不放行 `/publicity`。
- 刷新接口只接受 Refresh Token，不能经过 `AuthFunc`，必须放入白名单并在 handler 中调用 `Refresh`。
- 登录、刷新等公开接口在启用校验前必须显式加入白名单。

## 生命周期与边界

- 默认 Access 有效期 15 分钟、Refresh 7 天；可配置，要求 `1s <= access <= refresh`。
- 每次 Issue 创建随机 session ID；每对令牌共享一个随机 generation，各自具有独立 JTI。
- Redis 只保存会话当前 generation，TTL 对齐 Refresh 的到期时间。刷新通过 Lua 原子比较并替换 generation，同时立即撤销旧 Access 和旧 Refresh。无需逐个保存黑名单。
- 刷新重新起算两个有效期，属于滑动会话；当前不设置绝对登录时长上限。
- 同一个 Refresh 并发调用只有一个成功。旧 Refresh 重放会失败，但不会删除获胜的新会话。客户端应合并并发刷新请求，不能假定重试可返回上次结果。
- Logout 删除整个会话，可重复执行。为覆盖刷新并发，任何签名正确且未过期的旧版本会话令牌都可以登出当前会话。已过期的 Access 不能用于登出，可用未过期的 Refresh。
- Redis 不可用时操作返回错误，不降级放行。轮换已提交但响应丢失时，客户端可能需要重新登录；不提供跨网络的 exactly-once 交付保证。
- 撤销对后续认证检查生效，已经通过检查的在途请求不会被取消。
- **双令牌的 API 中间件必须调用 `SessionAuth.ParseAccess`**。直接调用底层 JWT 解析或旧 `JwtAuth.ParseClaims` 不会验证会话 generation，也不能替代用途检查。
- `SessionStorer` 是独立接口，不改变旧 `Storer`；自定义存储必须保证轮换相对轮换/删除原子化，且不得复活不存在或已过期的会话。
- `SessionAuth` 管理自己的 `SessionClaims`，因此 `NewSessionAuth` 遇到 `WithClaims` 会返回 `ErrSessionClaimsUnsupported`，而不是静默忽略。`WithClaims` 仍可用于旧 `JwtAuth`。

测试：在本目录执行 `go test -race ./...`。Redis 存储测试使用 miniredis 执行 Lua，覆盖原子轮换、过期与错误传播。
