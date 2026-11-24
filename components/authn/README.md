# authn

`JwtAuth` 保留原有单令牌签发、校验与吊销接口。`SessionAuth` 提供有状态的双令牌会话管理，JWT 签名与标准声明校验仍委托 `components/token`。

## 接入

```go
import redisstore "github.com/apus-run/gala/components/authn/store/redis"

sessionStore := redisstore.NewStore(redisClient, "myapp:auth:")
auth, err := authn.NewSessionAuth(sessionStore,
    authn.WithJWTOptions(token.WithSecretKey(secret), token.WithIssuer("myapp")),
    authn.WithSessionTTL(15*time.Minute, 7*24*time.Hour),
    authn.WithAbsoluteTTL(12*time.Hour), // 可选：整个会话最多 12 小时，到点强制重登
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

## 存储后端

`store` 只定义契约，实现按后端分包：

| 后端 | 构造 | 说明 |
| --- | --- | --- |
| Redis | `redisstore.NewStore(client, "myapp:")` | 接受任意 `redis.Cmdable`：单机、集群、ring |
| Postgres | `postgres.NewStore(db, prefix)` | 只依赖 `database/sql`，驱动自选；表名可加前缀 |
| MySQL / MariaDB | `mysqlstore.NewStore(db, prefix)` | 只依赖 `database/sql`；吊销令牌以 SHA-256 存储；表名可加前缀 |
| 内存 | `memory.NewStore()` | 测试与单进程；数据不跨重启/进程 |

Postgres 需要先建表：

```go
import (
    "database/sql"
    _ "github.com/jackc/pgx/v5/stdlib"

    "github.com/apus-run/gala/components/authn/store/postgres"
)

db, err := sql.Open("pgx", dsn)
// 检查 err 后：
st, err := postgres.NewStore(db, "")   // "" 表示无前缀；也可传 "app_" 等
// 检查 err 后：
if err := st.Migrate(ctx); err != nil { /* ... */ }
auth, err := authn.NewSessionAuth(st, opts...)
```

表名是 `login_sessions`（会话当前代次）与 `revoked_access_tokens`（吊销名单）。`NewStore` 接受可选前缀：传 `""` 用上面的原名，传 `"app_"` 则得到 `app_login_sessions` 等——多应用共用一个 schema 时用前缀隔离（也可以改用独立 schema/数据库）。前缀必须是 `[a-z_][a-z0-9_]{0,23}`，因为它会拼进 SQL 标识符；非法值在 `NewStore` 直接返回错误。**`Migrate` 是 store 的方法**，建表与查询共享同一套名字，不存在传错前缀的可能。

- 轮换是一条条件 `UPDATE ... WHERE generation = old AND expires_at > now()`，与 Redis 的 Lua CAS 语义一致：并发只有一个赢家，且不会复活已删除/过期的会话。
- 吊销表只存令牌本身（MySQL 存 SHA-256 摘要）与过期时间。`Storer.Set` 的 `val` 参数不落库——接口没有读回入口，只有存在性与过期可观察。
- `login_sessions.id` 就是 `SessionClaims.SessionID`，`generation` 是当前轮换代次；`expires_at` 同时兼任绝对上限与滑动续期的截止时间。
- 过期行不会自动删除。定期调用 `DeleteExpired(ctx)`，或交给数据库作业清理；查询本身已忽略过期行，清理只影响空间。
- `*sql.DB` 归调用方所有，`Store.Close` 是空操作。
- `NewStore` 接受该包导出的 `DB` 接口（`*sql.DB`、`*sql.Conn`、`*sql.Tx` 都满足），固定连接或在事务中使用均可。

MySQL 用法相同，换导入即可：

```go
import (
    "database/sql"
    _ "github.com/go-sql-driver/mysql"

    mysqlstore "github.com/apus-run/gala/components/authn/store/mysql"
)

db, err := sql.Open("mysql", dsn)
// 检查 err 后：
st, err := mysqlstore.NewStore(db, "")   // "" 表示无前缀；也可传 "app_" 等
// 检查 err 后：
if err := st.Migrate(ctx); err != nil { /* ... */ }
auth, err := authn.NewSessionAuth(st, opts...)
```

- **驱动必须保持 `clientFoundRows` 关闭**（go-sql-driver 默认关闭）：`CreateSession` 靠 `ON DUPLICATE KEY UPDATE` 的影响行数区分「插入 / 替换过期行 / 活跃行未动」。
- 吊销令牌以 SHA-256 摘要（`BINARY(32)`）入库，不落明文；Postgres/Redis 实现存的是原文，MySQL 在表结构层面拒绝了明文存储。

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

认证失败时中间件不会调用下游 handler。需要日志或指标时，用 `WithAuthErrorHook` 观察失败而不改变响应：

```go
mw := authn.NewMiddleware(authFn, authn.WithAuthErrorHook(func(ctx context.Context, r *http.Request, err error) {
    code, _ := authn.StatusOf(err)
    logger.WarnContext(ctx, "auth failed", "path", r.URL.Path, "status", code, "error", err)
}))
```

钩子在错误响应写出前调用，所以日志里能和客户端收到的状态码对上。

默认错误响应由 `StatusOf` 决定：

| 错误 | HTTP 状态 | `WWW-Authenticate` |
| --- | --- | --- |
| `authn.ErrMissingToken` | 401 | `Bearer` |
| `authn.ErrTokenInvalid`、`authn.ErrSessionInvalid` | 401 | `Bearer error="invalid_token"` |
| `authn.ErrCSRFMismatch` | 403 | 无 |
| 其他错误（例如存储不可用） | 503 | 无 |

401、403 与 503 都失败关闭。`StatusOf` 使用 `errors.Is`，所以包装后的错误也能正确分类。需要自定义响应体时，可用 `authn.WithErrorHandler` 替换默认响应。

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
| `TokenFromNamedHeader(name)` | 同名请求头，整段即令牌 | 不解析 `Bearer` 前缀；用于回传 `HeaderCarrier` 写出的头 |

所有内置来源都会去除首尾空白，空值视为没有令牌。

**取到令牌但校验失败时直接返回错误，不会再尝试后面的来源。**来源只负责"有没有"，不负责"对不对"：携带了无效 Bearer 头的请求不能借浏览器的 Cookie 通过认证。所有来源取到的令牌都走同一条 `ParseAccess`，用途校验、注销与存储故障的处理完全一致。

`TokenSource` 就是 `func(*http.Request) (string, bool)`，自定义来源（例如 `X-Access-Token` 头）直接写函数即可。

## 令牌交付

`Issue` / `Refresh` 只返回 `TokenPair`，怎么交给客户端由交付器（`authn.TokenCarrier`）决定。交付器只负责"写"，读取仍用上面的 `TokenSource`：

| 交付器 | 写入位置 | 适用场景 |
| --- | --- | --- |
| `HeaderCarrier` | 响应头 `X-Access-Token` / `X-Refresh-Token` | API 客户端（或网关统一透传），访问令牌再以 `Authorization: Bearer` 回传 |
| `CookieCarrier` | 两个 `HttpOnly` 令牌 cookie（默认 `access_token` / `refresh_token`）+ 一个可读的 `XSRF-TOKEN` | 浏览器；自带双提交 CSRF 防护，见下节 |

不想要任何交付器时，也可以直接把 `TokenPair` 序列化进 JSON 响应体（结构体带 json tag）。

`HeaderCarrier` 的读写是配对的——`Inject` 写出的头能被 `AccessSource` / `RefreshSource` 原样读回，适合客户端「把响应头存下来、下次原样回传」的场景：

```go
carrier := &authn.HeaderCarrier{}
carrier.Inject(w, pair)                     // 登录：X-Access-Token / X-Refresh-Token

// 中间件：读取访问令牌头，不带 Bearer 前缀
authFn := auth.AuthFunc(authn.TokenFromHeader, carrier.AccessSource())

// 刷新：读刷新令牌头
raw, ok := carrier.RefreshSource()(r)
next, err := auth.Refresh(ctx, raw)
carrier.Inject(w, next)                      // 轮换后重新交付

// 登出：撤销会话并清头
carrier.Clear(w)
```

```go
carrier := &authn.CookieCarrier{Secure: true, SameSite: http.SameSiteLaxMode}

// 登录：签发后交付
pair, err := auth.Issue(ctx, userID)
// 检查 err 后：
carrier.Inject(w, pair)

// 中间件同时接受 Bearer 头与访问 cookie，并强制 CSRF 双提交
authFn := carrier.Protect(auth.AuthFunc(authn.TokenFromHeader, carrier.AccessSource()))

// 刷新：先校验 CSRF，再从刷新 cookie 读取；轮换成功后重新交付
if err := carrier.VerifyCSRF(r); err != nil { /* 403 */ }
raw, ok := carrier.RefreshSource()(r)
next, err := auth.Refresh(ctx, raw)
carrier.Inject(w, next)

// 登出：撤销会话后清除
err = auth.Logout(ctx, accessToken)
carrier.Clear(w)
```

`CookieCarrier` 的令牌 cookie 始终带 `HttpOnly`；`SameSite=None` 时会自动带上 `Secure`，否则浏览器会直接丢弃。`MaxAge` 为 0 表示会话级 cookie，浏览器关闭即失效。读写名字必须一致——自定义名字时用 `AccessSource` / `RefreshSource` 生成来源，不要手写 `TokenFromCookie("...")`。

生产环境建议给令牌 cookie 加 `__Host-` 前缀（如 `__Host-access_token`）：carrier 会强制 `Secure`、`Path=/` 并忽略 `Domain`，浏览器据此拒绝子域名的同名覆盖。`__Secure-` 前缀只强制 `Secure`。

### CSRF 防护

Cookie 是环境凭据：浏览器会自动把它附到跨站请求上。`CookieCarrier.Inject` 因此在写入令牌的同时写一个**可读**的 `XSRF-TOKEN` cookie（`token.XSRFCookieName`），前端把它的值回填到 `X-XSRF-TOKEN` 头（`token.XSRFHeaderKey`）。每次 `Inject` / `Clear` 都会换新值。

`Protect` 和 `VerifyCSRF` 的规则：

- 只对**带令牌 cookie**的请求做校验；只发 `Authorization` 头的 API 客户端完全不受影响，因为跨站页面设不了请求头。
- 同时带令牌 cookie 和 Bearer 头的请求**仍要**回填 XSRF 头——判定只看请求是否携带 cookie，不看令牌最终来自哪个来源（失败关闭）。
- 安全方法（`GET` / `HEAD` / `OPTIONS`）不校验。
- 改状态请求必须有 `XSRF-TOKEN` cookie 且与 `X-XSRF-TOKEN` 头**完全相等**；任一缺失都拒绝，避免"清了一半"的浏览器悄悄失去防护。
- 认证先于 CSRF：未登录请求仍然是 401，不会被 CSRF 检查抢先变成 403。
- 失败返回 `authn.ErrCSRFMismatch`，`StatusOf` 映射为 **403**（无质询头）。它不是凭据错误，所以不和 `ErrTokenInvalid` 共用 401。

前端需要在每次登录/刷新后读取新的 `XSRF-TOKEN`（页面刷新即可）。登出时 `Clear` 一并清掉，下次登录重新种。

### 登录/登出的跨站防护

双提交校验依赖「已经有令牌 cookie」这个前提，而登录、登出恰好不满足：登录时浏览器还没有会话，攻击者可以伪造表单把**自己的账号**登进受害者的浏览器（login CSRF）。这两个端点用 `OriginGuard` 补齐：

```go
guard, err := authn.NewOriginGuard("https://app.example.com") // 前端与 API 同源时可省略参数
// 检查 err 后：
router.POST("/login", gin.WrapH(guard.Wrap(http.HandlerFunc(loginHandler))))
```

它包装标准库 `net/http.CrossOriginProtection`：

- `GET` / `HEAD` / `OPTIONS` 永远放行；
- 浏览器请求按 `Sec-Fetch-Site` 判定，没有该头时用 `Origin` 与请求 `Host` 比对；
- 两个头都没有的请求视为非浏览器客户端，放行——所以命令行和 SDK 调用不受影响；
- 只有前端与 API 不同源时才需要传信任来源，参数写 `scheme://host[:port]`，非法值会返回错误。

`Wrap` 命中时返回 403；也可以在 handler 里用 `guard.Check(r)` 自行决定响应。deny 响应体被固定为简短的 `Forbidden`——标准库默认会说明是哪个头不匹配，这对探测端点的攻击者有用。

### 生产检查清单

- JWT 密钥至少 32 字节（HS384 需 48、HS512 需 64）：`components/token` 在签发和解析时直接拒绝更短的密钥（`ErrWeakJWTSecret`），不会静默降级。
- 全站 HTTPS 并设 `Secure: true`；只有本地 http 调试可以关。
- `SameSite` 显式设为 `Lax` 或 `Strict`；`None` 会自动带 `Secure`，仅在确实需要跨站 cookie 时使用。
- 令牌 cookie 名加 `__Host-` 前缀，防止子域名覆盖；该前缀下 `Domain` 会被忽略。
- `XSRF-TOKEN` 按设计可读；前端在登录/刷新后重新读取，并在改状态请求里回填 `X-XSRF-TOKEN`。
- 刷新接口不在认证中间件后面，必须自行调用 `VerifyCSRF`。
- 同名的重复 `XSRF-TOKEN` cookie 会被拒绝（防 cookie tossing）；遇到 403 先检查是否存在同名的 Domain cookie。
- 双提交只比较 cookie 与请求头、不绑定服务端状态：它挡的是跨站伪造，不是 XSS。存在 XSS 时应先修 XSS。

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
- `NewBuilder` 接受任意 `authn.AuthFunc[T]`；浏览器 + CSRF 场景直接传 `carrier.Protect(authFn)`，错误仍由 `StatusOf` 映射（CSRF 失败为 403）。

## 生命周期与边界

- 默认 Access 有效期 15 分钟、Refresh 7 天；可配置，要求 `1s <= access <= refresh`。
- 每次 Issue 创建随机 session ID；每对令牌共享一个随机 generation，各自具有独立 JTI。
- Redis 只保存会话当前 generation，TTL 对齐 Refresh 的到期时间。刷新通过 Lua 原子比较并替换 generation，同时立即撤销旧 Access 和旧 Refresh。无需逐个保存黑名单。
- 刷新重新起算两个有效期，属于滑动会话。可用 `authn.WithAbsoluteTTL(d)` 加一个绝对会话上限：

```go
auth, err := authn.NewSessionAuth(store, opts...,
    authn.WithSessionTTL(15*time.Minute, 7*24*time.Hour),
    authn.WithAbsoluteTTL(12*time.Hour), // 登录 12 小时后必须重新登录
)
```

`login_at` 声明记录会话起点，轮换时原样传递，所以刷新不会重置绝对时钟。两个令牌的 `exp` 在签发时就被截止时间封顶，会话键 TTL 同步缩短——仅凭令牌本身即可落实上限，不依赖存储。超过上限后 `Refresh` 与 `ParseAccess` 都返回 `ErrSessionInvalid`。

- 绝对上限默认关闭（`0`）。开启后，此前签发的令牌因缺少 `login_at` 无法界定寿命，会被判为 `ErrTokenInvalid`，存量会话需要一次性重新登录；关闭状态下 `login_at` 仍会写入，便于日后开启时只影响更早的令牌。
- `WithAbsoluteTTL` 要求为 `0` 或至少 `1s`。
- 运行中调整该值：调小会立即约束存量令牌（`parse` 按当前值重新比对 `login_at`）；调大不会延长已签发令牌的寿命，已封顶的 `exp` 保持不变，只对之后签发的令牌生效。
- 同一个 Refresh 并发调用只有一个成功。旧 Refresh 重放会失败，但不会删除获胜的新会话。客户端应合并并发刷新请求，不能假定重试可返回上次结果。
- Logout 删除整个会话，可重复执行。为覆盖刷新并发，任何签名正确且未过期的旧版本会话令牌都可以登出当前会话。已过期的 Access 不能用于登出，可用未过期的 Refresh。
- Redis 不可用时操作返回错误，不降级放行。轮换已提交但响应丢失时，客户端可能需要重新登录；不提供跨网络的 exactly-once 交付保证。
- 撤销对后续认证检查生效，已经通过检查的在途请求不会被取消。
- **双令牌的 API 中间件必须调用 `SessionAuth.ParseAccess`**。直接调用底层 JWT 解析或旧 `JwtAuth.ParseClaims` 不会验证会话 generation，也不能替代用途检查。
- `SessionStorer` 是独立接口，不改变旧 `Storer`；自定义存储必须保证轮换相对轮换/删除原子化，且不得复活不存在或已过期的会话。
- **存储实现按后端分包**：`store` 只放契约（`Storer` / `SessionStorer`），内置实现位于 `store/redis`（任意 `redis.Cmdable`，单机/集群/ring）、`store/postgres` 与 `store/mysql`（都只用 `database/sql`）、`store/memory`（测试与单进程）。其他后端只需在自有包里实现四个方法并交给 `NewSessionAuth`。
- `memory.NewStore()` 遵循同一契约，数据不跨重启、不跨进程，生产用 `redis.NewStore`。
- 非法 TTL 由契约统一为 `store.ErrInvalidTTL`（`CreateSession` 收到非正值）与 `store.ErrTTLTooSmall`（`RotateSession` 小于 1ms），所有后端行为一致，可用 `errors.Is` 判断。
- **自定义 `SessionStorer` 必须跑 `store/storetest`**：`storetest.SessionStorer(t, storetest.Options{New: ...})` 会验证比较并写入、轮换原子性、删除/过期不复活与并发唯一赢家。内置的 memory 与 redis 实现都跑同一套。`Clock` 让能快进时钟的后端（如 miniredis 的 `FastForward`）免去真实等待。
- `SessionAuth` 管理自己的 `SessionClaims`，因此 `NewSessionAuth` 遇到 `WithClaims` 会返回 `ErrSessionClaimsUnsupported`，而不是静默忽略。`WithClaims` 仍可用于旧 `JwtAuth`。

测试：在本目录执行 `go test -race ./...`。`memory`、`redis`、`postgres`、`mysql` 都跑同一套 `storetest` 契约；SQL 集成测试需要 `AUTHN_POSTGRES_DSN` / `AUTHN_MYSQL_DSN`，各自使用一次性 schema/数据库，不碰其他数据，未设置时显式跳过。`SessionAuth` 的时间源可注入（未导出），绝对上限相关测试直接拨时钟而不用 sleep。
