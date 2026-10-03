# Helmet

为 Gin 响应添加常见安全头，移植自 [Fiber v3 Helmet](https://github.com/gofiber/fiber/tree/main/middleware/helmet)，保留其配置字段、默认值、跳过逻辑、CSP 模式和 HSTS 参数规则。Gin 版使用无状态 Builder 构建处理器，并增加 `IsSecure` 回调支持应用自己的 HTTPS 判定。

当前 ginx 模块要求 **Go 1.25 或更高版本**，不依赖 Fiber。上游用法可参考 [Fiber Helmet 文档](https://docs.gofiber.io/next/middleware/helmet/)，Gin 接口和行为以本目录源码及本文为准。移植代码保留上游版权声明和 [MIT License](LICENSE)。现有移植未记录上游提交号，链接用于查阅来源，不代表始终与上游最新版本一致。

## 安装与接口

```sh
go get github.com/apus-run/gala/components/ginx
```

```go
type Builder struct{}

func NewBuilder() *Builder
func (b *Builder) Build(config ...Config) gin.HandlerFunc
```

导入路径为 `github.com/apus-run/gala/components/ginx/middlewares/helmet`。示例对应当前仓库源码；使用尚未发布的新增包时，可在应用 go.mod 中用 replace 指向本地 `components/ginx` 模块。

采用无状态 Builder：NewBuilder 只返回 `&Builder{}`。Build 没有参数时使用 ConfigDefault；传入配置时只使用第一个 Config，后续参数被忽略，与上游配置语义一致。每次 Build 独立保存配置，不保留请求状态，也不创建后台任务。

## 基础示例

```go
package main

import (
    "log"
    "net/http"

    "github.com/gin-gonic/gin"
    "github.com/apus-run/gala/components/ginx/middlewares/helmet"
)

func main() {
    router := gin.New()
    router.Use(helmet.NewBuilder().Build(), gin.Recovery())
    router.GET("/", func(c *gin.Context) {
        c.String(http.StatusOK, "Welcome!")
    })
    if err := router.Run("127.0.0.1:3000"); err != nil {
        log.Fatal(err)
    }
}
```

查看响应头：

```sh
curl -i http://127.0.0.1:3000/
```

此示例只注册 GET。`curl -I` 会发送 HEAD，如需使用它，请另外注册 HEAD 路由。默认会写入 11 个响应头；默认不启用 CSP、Permissions-Policy 或 HSTS。

## 配置

上游 18 个字段全部保留，Next 的参数类型适配为 `*gin.Context`。IsSecure 是 Gin 版额外的 HTTPS 判定入口。

| 属性 | 类型 | 说明 | 默认值 |
| --- | --- | --- | --- |
| `Next` | `func(*gin.Context) bool` | 返回 true 时跳过 Helmet，继续后续处理链 | `nil` |
| `XSSProtection` | `string` | `X-XSS-Protection` | `"0"` |
| `ContentTypeNosniff` | `string` | `X-Content-Type-Options` | `"nosniff"` |
| `XFrameOptions` | `string` | `X-Frame-Options` | `"SAMEORIGIN"` |
| `HSTSMaxAge` | `int` | HSTS 的 max-age，单位为秒 | `0` |
| `HSTSExcludeSubdomains` | `bool` | 不添加 HSTS 的 includeSubDomains 指令 | `false` |
| `ContentSecurityPolicy` | `string` | CSP 策略内容 | `""` |
| `CSPReportOnly` | `bool` | 将 CSP 写入报告模式响应头 | `false` |
| `HSTSPreloadEnabled` | `bool` | 添加 HSTS 的 preload 指令 | `false` |
| `ReferrerPolicy` | `string` | `Referrer-Policy` | `"no-referrer"` |
| `PermissionPolicy` | `string` | `Permissions-Policy` | `""` |
| `CrossOriginEmbedderPolicy` | `string` | `Cross-Origin-Embedder-Policy` | `"require-corp"` |
| `CrossOriginOpenerPolicy` | `string` | `Cross-Origin-Opener-Policy` | `"same-origin"` |
| `CrossOriginResourcePolicy` | `string` | `Cross-Origin-Resource-Policy` | `"same-origin"` |
| `OriginAgentCluster` | `string` | `Origin-Agent-Cluster` | `"?1"` |
| `XDNSPrefetchControl` | `string` | `X-DNS-Prefetch-Control` | `"off"` |
| `XDownloadOptions` | `string` | `X-Download-Options` | `"noopen"` |
| `XPermittedCrossDomain` | `string` | `X-Permitted-Cross-Domain-Policies` | `"none"` |
| `IsSecure` | `func(*gin.Context) bool` | HTTPS 判定回调，仅启用 HSTS 时调用 | `nil`，使用实际 TLS 连接 |

### 默认值与空值

有非空默认值的 11 个字符串字段显式设为 `""` 时，仍会补上 ConfigDefault 的对应值，与上游一致；空字符串不是这些字段的关闭开关。CSP 和 Permissions-Policy 默认不写入，只有配置为非空才会写入。

Build 按值保存配置，之后修改原 Config 或 ConfigDefault 不影响已生成的处理器。回调函数自身被复制，但它捕获的外部变量不会被深拷贝；Next 和 IsSecure 回调及其访问的共享状态必须并发安全，不应保留 gin.Context。ConfigDefault 是可修改的全局变量，应在初始化阶段设置，不能与 Build 并发修改。

`Build()` 和 `Build(Config{})` 在未修改 ConfigDefault 时等价。修改 ConfigDefault 后，它们不一定等价，处理规则如下：

| 调用 | 使用的配置 | HSTS 参数校验 |
| --- | --- | --- |
| `Build()` | 完整复制当前 ConfigDefault，包括回调、布尔值、HSTS、CSP 和 Permissions-Policy | 不执行 |
| `Build(cfg)` / `Build(Config{})` | 复制第一个配置，只为上述 11 个空字符串字段补默认值；其它字段保留传入值或零值 | 执行 |

例如在 ConfigDefault 中设置 HSTSMaxAge 或 Next 后，`Build(Config{})` 不会继承它们。需要在全局默认值上覆盖少数字段时，先复制完整配置，再修改：

```go
cfg := helmet.ConfigDefault
cfg.XFrameOptions = "DENY"
router.Use(helmet.NewBuilder().Build(cfg))
```

该写法也会执行 HSTS 参数校验。零值 `var builder helmet.Builder` 可直接调用 Build，无需先调用 NewBuilder。

```go
var ConfigDefault = Config{
    XSSProtection:             "0",
    ContentTypeNosniff:        "nosniff",
    XFrameOptions:             "SAMEORIGIN",
    ReferrerPolicy:            "no-referrer",
    CrossOriginEmbedderPolicy: "require-corp",
    CrossOriginOpenerPolicy:   "same-origin",
    CrossOriginResourcePolicy: "same-origin",
    OriginAgentCluster:        "?1",
    XDNSPrefetchControl:       "off",
    XDownloadOptions:          "noopen",
    XPermittedCrossDomain:     "none",
}
```

### 参数校验

显式传入的配置在 Build 时按上游规则校验：

- HSTSMaxAge 为负数会 panic：`helmet: HSTSMaxAge must be greater than or equal to 0`。
- 同时启用 HSTSPreloadEnabled 和 HSTSExcludeSubdomains 会 panic：`helmet: HSTSPreloadEnabled requires HSTSExcludeSubdomains to be false`。

无参数 `Build()` 直接复制 ConfigDefault，不执行上述校验；这是保留的上游行为。若在初始化时修改全局 HSTS 默认值，建议用 `Build(helmet.ConfigDefault)` 显式触发校验。

策略字符串直接用作响应头值，本包不解析或补充策略指令，也不检查策略语法或 preload 的部署条件。策略应来自应用配置，避免将未经校验的请求输入拼接进响应头。

## 自定义安全头

下面的片段假定已有 router，并导入示例使用的包。

```go
router.Use(helmet.NewBuilder().Build(helmet.Config{
    XFrameOptions:         "DENY",
    ContentSecurityPolicy: "default-src 'self'; object-src 'none'; frame-ancestors 'none'",
    ReferrerPolicy:        "strict-origin-when-cross-origin",
    PermissionPolicy:      "camera=(), microphone=(), geolocation=()",
}))
```

安全头在调用后续处理器前写入。Helmet 不改变业务响应状态、正文或错误处理；后续处理器仍可覆盖或删除响应头。

### 挂载顺序与已有响应头

在注册路由前安装 Helmet，并放在可能提前响应、Abort 或处理 panic 的中间件之前，例如基础示例中的 `Helmet → Recovery → 业务处理器`。这样后续认证拒绝和 Recovery 生成的响应也能带上安全头。已经写出响应后，再修改响应头不会生效。

| 情形 | 行为 |
| --- | --- |
| Helmet 配置了某个头，包括补齐默认值的头 | 覆盖此前该头的值 |
| 未配置 CSP 或 Permissions-Policy | 保留已有值 |
| HSTS 为 0 或请求未被判定为 HTTPS | 保留已有 HSTS 值，不删除 |
| Next 返回 true | 保留此前所有头，直接继续后续链 |
| 后续中间件在写响应前修改头 | 后续设置生效 |

只有执行到 Helmet 的响应才有这些保证。Gin 的自动尾斜杠或路径修正重定向不执行路由处理链；这类响应若也需要安全头，可在网关或 Gin 外层的 `net/http` 中间件统一处理。

### 按路由调整浏览器策略

默认 `Cross-Origin-Embedder-Policy: require-corp` 会约束跨源资源加载；`Cross-Origin-Opener-Policy: same-origin` 可能切断与跨源弹窗的 opener 关系。使用第三方资源、登录弹窗或支付弹窗的页面，需要按业务选择策略。可参考 HTML 标准中的 [COEP](https://html.spec.whatwg.org/multipage/browsers.html#cross-origin-embedder-policies) 和 [COOP](https://html.spec.whatwg.org/multipage/browsers.html#cross-origin-opener-policies)。

例如为允许加载跨源资源的页面单独挂载：

```go
pages := router.Group("/pages", helmet.NewBuilder().Build(helmet.Config{
    CrossOriginEmbedderPolicy: "unsafe-none",
    CrossOriginOpenerPolicy:   "same-origin-allow-popups",
}))
pages.GET("/login", func(c *gin.Context) {
    c.String(http.StatusOK, "Login page")
})
```

这些是显式的浏览器策略值，空字符串仍会恢复默认值。如果确实需要移除某个默认头，可在 Helmet 之后、业务写响应之前删除：

```go
router.Use(helmet.NewBuilder().Build())
router.Use(func(c *gin.Context) {
    c.Header("X-Frame-Options", "") // Gin 的空值 Header 调用会删除该头。
    c.Next()
})
```

是否移除由应用的嵌入策略决定。Helmet 不设置 `Access-Control-Allow-*`，跨源 API 访问仍需单独配置 CORS。

## 跳过指定请求

```go
router.Use(helmet.NewBuilder().Build(helmet.Config{
    Next: func(c *gin.Context) bool {
        return c.Request.URL.Path == "/health"
    },
}))
```

返回 true 时，不设置任何 Helmet 响应头，也不执行 IsSecure，仍会调用后续处理链。该逻辑只跳过 Helmet，不跳过其它认证、授权或业务中间件。

## CSP 报告模式

```go
router.Use(helmet.NewBuilder().Build(helmet.Config{
    ContentSecurityPolicy: "default-src 'self'; report-uri /csp-report",
    CSPReportOnly:         true,
}))
```

默认模式写入 `Content-Security-Policy`；CSPReportOnly 为 true 时写入 `Content-Security-Policy-Report-Only`。两种模式写入相同的策略内容。

与上游一致，Helmet 只设置选中的响应头，不删除之前已有的另一种 CSP 响应头。例如已有强制 CSP 时，开启报告模式不会移除那条强制策略。本包也不创建报告接收端点。

## HSTS

```go
router.Use(helmet.NewBuilder().Build(helmet.Config{
    HSTSMaxAge:         31536000,
    HSTSPreloadEnabled: true,
}))
```

仅在请求被判定为 HTTPS 且 HSTSMaxAge 大于 0 时设置 Strict-Transport-Security：

| 配置 | 响应头值 |
| --- | --- |
| `HSTSMaxAge: 60` | `max-age=60; includeSubDomains` |
| `HSTSMaxAge: 60, HSTSExcludeSubdomains: true` | `max-age=60` |
| `HSTSMaxAge: 31536000, HSTSPreloadEnabled: true` | `max-age=31536000; includeSubDomains; preload` |

值在 Build 时构造一次；不会在每次请求重新格式化。HSTSMaxAge 为 0 时不设置该头，也不会发送 `max-age=0`。这不会清除浏览器已经缓存的 HSTS 策略；需要撤销动态策略时，应在 HTTPS 响应中另行发送 `Strict-Transport-Security: max-age=0`，参见 [RFC 6797 §6.1.1](https://www.rfc-editor.org/rfc/rfc6797.html#section-6.1.1)。PreloadEnabled 只添加响应指令，不执行域名注册，也不处理 preload 列表的撤销。

Helmet 不配置 TLS、不把 HTTP 重定向到 HTTPS；HTTPS 服务和跳转应由应用或网关负责。

### HTTPS 与反向代理

上游 Helmet 使用 Fiber 的 Secure，可信代理的配置属于 Fiber 应用。Gin 版默认使用 `Request.TLS != nil`，与上游未启用可信代理时的行为一致；URL scheme 或客户端自行提供的 X-Forwarded-* 不能使请求自动被判定为 HTTPS。

TLS 在可信网关终止时，可以通过 IsSecure 对接应用的代理策略。示例只接受指定网段的直接连接来源，并读取其 X-Forwarded-Proto；部署时替换成实际代理地址范围，并保证应用只能通过这些可信代理访问。网关必须覆盖客户端提供的该请求头；仅追加值可能保留攻击者伪造的首项。多级代理需结合实际转发链调整判定，不能只靠网段和首项协议做通用推断。

```go
trustedProxy := netip.MustParsePrefix("10.0.0.0/8")
router.Use(helmet.NewBuilder().Build(helmet.Config{
    HSTSMaxAge: 31536000,
    IsSecure: func(c *gin.Context) bool {
        if c.Request.TLS != nil {
            return true
        }
        peer, err := netip.ParseAddr(c.RemoteIP())
        if err != nil || !trustedProxy.Contains(peer.Unmap()) {
            return false
        }
        proto, _, _ := strings.Cut(c.GetHeader("X-Forwarded-Proto"), ",")
        return strings.EqualFold(strings.TrimSpace(proto), "https")
    },
}))
```

补充导入 `net/netip` 和 `strings`。这里用 RemoteIP 检查直接连接来源，不能替换成可能取自转发头的 ClientIP；Gin 的 SetTrustedProxies 配置不会自动为 Helmet 提供 HTTPS 判定。IsSecure 在同步请求链中执行，只有 HSTSMaxAge 大于 0 且请求未跳过 Helmet 时才会调用。

## 能力对齐与验证

- 上游 18 个 Config 字段、ConfigDefault 和可变配置参数语义全部保留，通过 NewBuilder().Build(config...) 构建。
- 11 个默认响应头、CSP 两种模式、Permissions-Policy、HSTS 的子域与 preload 指令全部保留。
- `helmet_test.go` 包含 19 个顶层测试，覆盖默认头、自定义头、CSP、Permissions-Policy、HSTS 参数和 Gin 适配行为；HSTS 使用实际 TLS 测试服务验证。
- Gin 版额外测试配置快照、跳过全部响应头、伪造 scheme、可信代理及已有响应头的保留行为。
- Gin 版采用 NewBuilder/Build 构造，适配处理器/回调类型和响应头写入 API，并增加 IsSecure 对接框架之外的可信代理策略；不引入 Fiber 依赖。

在 `components/ginx` 模块中运行：

```sh
go test -race -cover ./middlewares/helmet
go vet ./middlewares/helmet
```

测试验证服务端响应头和处理链行为；浏览器是否执行策略、反向代理是否正确覆盖协议头，以及 preload 注册状态，需要在实际部署环境验证。
