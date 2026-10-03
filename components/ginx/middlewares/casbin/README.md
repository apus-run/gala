# Casbin Gin 中间件

为 Gin 提供路由权限、权限列表和角色列表校验。模型、策略存储及重载由 [authz](../../../authz/README.md) 或应用持有的 Enforcer 管理；本包负责读取已认证的主体、执行授权和返回 HTTP 响应。

## 安装

要求 **Go 1.25 或更高版本**。

```sh
go get github.com/apus-run/gala/components/ginx
```

使用 Gala authz 时同时安装：

```sh
go get github.com/apus-run/gala/components/authz
```

下面示例对应当前源码。使用尚未发布的变更时，通过 `go.mod` 的 `replace` 指向本地 ginx、authz 模块。

## 接口签名

```go
func NewBuilder() *Builder

func (b *Builder) Build() (gin.HandlerFunc, error)
func (b *Builder) RoutePermission() (gin.HandlerFunc, error)
func (b *Builder) RequiresPermissions(permissions []string, opts ...Option) (gin.HandlerFunc, error)
func (b *Builder) RequiresRoles(roles []string, opts ...Option) (gin.HandlerFunc, error)
```

所有生成方法都返回错误，应在注册路由前检查。`Build()` 与 `RoutePermission()` 等价。

## Builder 配置

| 方法 | 参数类型 | 说明 | 默认值 |
| --- | --- | --- | --- |
| `SetEnforcer` | `Enforcer` | 并发安全的授权器，实现 `Enforce(...any) (bool, error)` | 必填 |
| `SetLookup` | `func(*gin.Context) (string, error)` | 读取前置认证已经确认的主体；空字符串表示无身份 | 必填 |
| `SetSkip` | `func(*gin.Context) bool` | 返回 true 时跳过当前授权校验 | 不跳过 |
| `SetRequestValues` | `func(*gin.Context, string) ([]any, error)` | 自定义 RoutePermission 的请求字段 | 主体、URL.Path、HTTP Method |
| `SetUnauthorized` | `gin.HandlerFunc` | 主体为空时的响应 | HTTP 401，空正文 |
| `SetForbidden` | `gin.HandlerFunc` | 授权拒绝时的响应 | HTTP 403，空正文 |
| `SetErrorHandler` | `func(*gin.Context, error)` | 主体读取、字段提取或授权执行失败时的响应 | HTTP 500，空正文 |

非空角色校验还要求 Enforcer 实现：

```go
type RoleGetter interface {
    GetRolesForUser(name string, domain ...string) ([]string, error)
}
```

`authz.Authorizer` 已提供这一能力。自定义授权器可按自身角色存储实现接口。

## Option

| 选项 | 说明 | 默认值 |
| --- | --- | --- |
| `WithValidationRule(ValidationRule)` | `MatchAllRule` 要求全部满足，`AtLeastOneRule` 要求至少一项满足 | `MatchAllRule` |
| `WithPermissionParser(PermissionParserFunc)` | 将权限字符串拆成 Enforce 中主体之后的字段；角色校验不调用解析器 | `PermissionParserWithSeparator(":")` |

每个 Handler 独立创建 Options，选项按顺序应用。非法 ValidationRule 或 nil 解析器会在生成 Handler 时返回错误；不要传入 nil Option。

## 快速开始

先从 [authz 示例](../../../authz/README.md#模型与策略文件) 复制 `model.conf`、`policy.csv` 到程序工作目录。示例模型默认拒绝，策略允许 alice 以 editor 角色创建、删除博客。

下面程序注册三种授权方式，并提供一个直接角色校验的拒绝案例。为了独立演示，认证中间件验证固定的示例令牌 `Bearer demo-alice` 后写入主体；接入应用时替换为实际 JWT、Session 或其它认证模块。

```go
package main

import (
    "errors"
    "log"
    "net/http"

    "github.com/apus-run/gala/components/authz"
    casbinmw "github.com/apus-run/gala/components/ginx/middlewares/casbin"
    "github.com/gin-gonic/gin"
)

func main() {
    if err := run(); err != nil {
        log.Fatal(err)
    }
}

func run() error {
    az, err := authz.New()
    if err != nil {
        return err
    }

    router := gin.New()
    router.Use(gin.Recovery(), authenticateDemo)
    builder := casbinmw.NewBuilder().SetEnforcer(az).SetLookup(lookupSubject)

    create, err := builder.RequiresPermissions([]string{"blog:create"})
    if err != nil {
        return err
    }
    route, err := builder.RoutePermission()
    if err != nil {
        return err
    }
    editor, err := builder.RequiresRoles([]string{"editor"})
    if err != nil {
        return err
    }
    admin, err := builder.RequiresRoles([]string{"admin"})
    if err != nil {
        return err
    }

    router.POST("/blog", create, func(c *gin.Context) {
        c.JSON(http.StatusCreated, gin.H{"message": "created"})
    })
    router.DELETE("/blog/:id", route, func(c *gin.Context) {
        c.Status(http.StatusNoContent)
    })
    router.PUT("/blog/:id", editor, func(c *gin.Context) {
        c.Status(http.StatusNoContent)
    })
    router.POST("/blog/publish", admin, func(c *gin.Context) {
        c.Status(http.StatusNoContent)
    })
    return router.Run("127.0.0.1:8080")
}

func authenticateDemo(c *gin.Context) {
    if c.GetHeader("Authorization") != "Bearer demo-alice" {
        c.AbortWithStatus(http.StatusUnauthorized)
        return
    }
    c.Set("subject", "alice")
    c.Next()
}

func lookupSubject(c *gin.Context) (string, error) {
    value, exists := c.Get("subject")
    if !exists {
        return "", nil
    }
    subject, ok := value.(string)
    if !ok {
        return "", errors.New("invalid authenticated subject type")
    }
    return subject, nil
}
```

调用示例：

```sh
# 权限列表校验：201
curl -i -X POST -H 'Authorization: Bearer demo-alice' http://127.0.0.1:8080/blog

# 路由权限校验：204
curl -i -X DELETE -H 'Authorization: Bearer demo-alice' http://127.0.0.1:8080/blog/42

# 直接角色 editor：204
curl -i -X PUT -H 'Authorization: Bearer demo-alice' http://127.0.0.1:8080/blog/42

# alice 的直接角色不包含 admin：403
curl -i -X POST -H 'Authorization: Bearer demo-alice' http://127.0.0.1:8080/blog/publish

# 未认证：401
curl -i -X POST http://127.0.0.1:8080/blog
```

后续代码片段复用快速开始中的 `builder` 和 `router`。

## CustomPermission：权限列表

每条权限由解析器转换为请求字段，主体由 Lookup 提供。默认 `blog:create` 对应：

```go
Enforce(subject, "blog", "create")
```

默认要求全部权限满足：

```go
handler, err := builder.RequiresPermissions([]string{"blog:create", "blog:delete"})
if err != nil {
    return err
}
router.POST("/blog/import", handler, func(c *gin.Context) {
    c.Status(http.StatusNoContent)
})
```

至少一项满足：

```go
handler, err := builder.RequiresPermissions(
    []string{"blog:create", "blog:delete"},
    casbinmw.WithValidationRule(casbinmw.AtLeastOneRule),
)
if err != nil {
    return err
}
router.POST("/blog/import", handler, func(c *gin.Context) {
    c.Status(http.StatusNoContent)
})
```

改变分隔符：

```go
handler, err := builder.RequiresPermissions(
    []string{"blog/create"},
    casbinmw.WithPermissionParser(casbinmw.PermissionParserWithSeparator("/")),
)
if err != nil {
    return err
}
```

自定义解析器需要返回与模型一致的字段数。解析器使用 strings.Split 的行为，保留空字段；本包不强制权限必须拆成两段。参数数量不符合模型时，授权器会返回执行错误。

## RoutePermission：路由权限

默认按主体、实际 URL 路径和 HTTP 方法授权：

```go
handler, err := builder.RoutePermission()
if err != nil {
    return err
}
router.DELETE("/blog/:id", handler, func(c *gin.Context) {
    c.Status(http.StatusNoContent)
})
```

请求 `/blog/42?force=true` 会调用 `Enforce(subject, "/blog/42", "DELETE")`。query 不进入路径字段，路径使用 URL.Path。

需要按 Gin 路由模板授权时，可在创建 Handler 前自定义请求字段：

```go
builder.SetRequestValues(func(c *gin.Context, subject string) ([]any, error) {
    path := c.FullPath()
    if path == "" {
        path = c.Request.URL.Path
    }
    return []any{subject, path, c.Request.Method}, nil
})
```

`SetRequestValues` 仅影响之后创建的路由权限 Handler；权限列表和角色校验使用各自的参数来源。OPTIONS 请求不会自动跳过，可通过 `SetSkip` 配置：

```go
builder.SetSkip(func(c *gin.Context) bool {
    return c.Request.Method == http.MethodOptions
})
```

Skip 只跳过授权，前置认证中间件仍按其自身规则执行。

## RoleAuthorization：角色列表

只匹配 `GetRolesForUser` 返回的直接角色，默认要求全部满足：

```go
handler, err := builder.RequiresRoles([]string{"editor"})
if err != nil {
    return err
}
router.PUT("/blog/:id", handler, func(c *gin.Context) {
    c.Status(http.StatusNoContent)
})
```

任意一个角色满足：

```go
handler, err := builder.RequiresRoles(
    []string{"admin", "editor"},
    casbinmw.WithValidationRule(casbinmw.AtLeastOneRule),
)
if err != nil {
    return err
}
```

每个请求只查询一次角色；不按角色名调用 Enforce，也不展开间接角色。示例策略 `alice → editor → admin` 下，`RequiresRoles(["editor"])` 通过，`RequiresRoles(["admin"])` 拒绝。

角色查询使用默认域。多租户应用应注入绑定到已确定域的 RoleGetter，或使用模型对应的资源授权；中间件不会从客户端字段自动推测域。

## HTTP 响应与校验顺序

| 情况 | 行为 |
| --- | --- |
| 构造依赖缺失、规则非法或解析器为 nil | 返回构造错误，应在注册路由前处理 |
| 构造成功后的空权限或角色列表 | 直接放行，不执行 Lookup 或授权器查询 |
| Skip 返回 true | 直接放行，跳过当前授权校验 |
| Lookup 返回空主体 | 401 |
| Lookup 返回错误 | 500 |
| 授权拒绝 | 403 |
| 授权器、角色查询或请求字段提取返回错误 | 500 |
| 授权通过 | 执行后续 Handler |

列表按输入顺序校验。all 在首次拒绝时停止，any 在首次允许时停止；遇到执行错误立即返回 500。空列表和 Skip 都在 Lookup 前处理，身份认证仍由前置中间件负责。

自定义失败响应：

```go
builder.
    SetUnauthorized(func(c *gin.Context) {
        c.JSON(http.StatusUnauthorized, gin.H{"message": "authentication required"})
    }).
    SetForbidden(func(c *gin.Context) {
        c.JSON(http.StatusForbidden, gin.H{"message": "permission denied"})
    }).
    SetErrorHandler(func(c *gin.Context, err error) {
        log.Printf("authorization failed: %v", err)
        c.JSON(http.StatusInternalServerError, gin.H{"message": "authorization failed"})
    })
```

失败回调调用前，Gin 请求链已经 Abort。默认响应为空正文，原始错误仅交给 ErrorHandler；示例响应不把内部错误发送给客户端。

## 复用与自定义选项

Builder 在构造 Handler 时复制自身配置和权限/角色列表；后续 Setter 或原始列表修改不会改变已生成 Handler。Enforcer、回调和解析器仍引用调用方提供的对象，必须满足并发请求的使用方式。Setter 和构造方法应在启动阶段调用，不与请求处理并发修改 Builder。

自定义组合选项通过 OptionFunc 与 Apply 实现：

```go
combined := casbinmw.OptionFunc(func(o *casbinmw.Options) {
    o.Apply(
        casbinmw.WithValidationRule(casbinmw.AtLeastOneRule),
        casbinmw.WithPermissionParser(casbinmw.PermissionParserWithSeparator("/")),
    )
})
```

这些选项用于 Handler 构造，不应保留 Options 用于运行时修改。回调在请求 goroutine 上执行，不应保留 gin.Context 供异步使用。
