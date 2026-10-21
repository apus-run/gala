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

以上为调用片段，每次调用都必须检查错误。`WithClaims` 不用于 `SessionAuth`；它管理自己的 `SessionClaims`。业务角色、用户禁用检查由上层完成；刷新前若需要读取身份，可先用 `components/token` 校验 Refresh Token 的签名并读取 subject，最终仍必须调用 `Refresh` 完成用途检查与原子消费。

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

测试：在本目录执行 `go test -race ./...`。Redis 存储测试使用 miniredis 执行 Lua，覆盖原子轮换、过期与错误传播。
