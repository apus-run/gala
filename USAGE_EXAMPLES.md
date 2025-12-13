# Gala 模块使用示例

本文档提供了 Gala 项目中各模块的使用示例。

## 快速开始

### 安装依赖

```bash
# 添加数据库组件
go get github.com/apus-run/gala/components/db@v0.5.2

# 添加错误处理工具
go get github.com/apus-run/gala/pkg/errorsx@v0.5.2

# 添加配置管理
go get github.com/apus-run/gala/components/conf@v0.5.2

# 添加日志组件
go get github.com/apus-run/gala/components/log@v0.5.2
```

### 基础使用示例

```go
package main

import (
    "context"

    "github.com/apus-run/gala/components/db"
    "github.com/apus-run/gala/pkg/errorsx"
)

func main() {
    // 使用数据库组件
    repo := db.NewRepository()

    // 使用错误处理
    if err := process(); err != nil {
        errorsx.Handle(err)
    }
}

func process() error {
    return errorsx.New("示例错误")
}
```

## 组件使用示例

### 1. 数据库组件 (components/db)

**功能**: 数据库操作封装

**安装**:
```bash
go get github.com/apus-run/gala/components/db@v0.5.2
```

**使用**:
```go
package main

import (
    "context"

    "github.com/apus-run/gala/components/db"
)

type User struct {
    ID   int64  `json:"id"`
    Name string `json:"name"`
}

func main() {
    // 初始化数据库仓库
    repo := db.NewRepository()

    // 创建用户
    user := &User{
        Name: "张三",
    }

    ctx := context.Background()
    if err := repo.Create(ctx, user); err != nil {
        panic(err)
    }
}
```

### 2. 错误处理工具 (pkg/errorsx)

**功能**: 错误处理和包装

**安装**:
```bash
go get github.com/apus-run/gala/pkg/errorsx@v0.5.2
```

**使用**:
```go
package main

import (
    "errors"
    "fmt"

    "github.com/apus-run/gala/pkg/errorsx"
)

func main() {
    // 创建自定义错误
    err := errorsx.New("业务错误")
    err = errorsx.Wrap(err, "包装错误信息")

    // 检查错误类型
    if errorsx.Is(err, "业务错误") {
        fmt.Println("找到目标错误")
    }

    // 格式化输出
    fmt.Printf("错误详情: %v\n", err)
}
```

### 3. 配置管理 (components/conf)

**功能**: 配置加载和管理

**安装**:
```bash
go get github.com/apus-run/gala/components/conf@v0.5.2
```

**使用**:
```go
package main

import (
    "fmt"

    "github.com/apus-run/gala/components/conf"
)

type Config struct {
    Server ServerConfig `json:"server"`
    DB     DBConfig     `json:"db"`
}

type ServerConfig struct {
    Port int    `json:"port"`
    Host string `json:"host"`
}

type DBConfig struct {
    Host string `json:"host"`
    Port int    `json:"port"`
}

func main() {
    // 加载配置
    config := &Config{}
    if err := conf.Load("config.yaml", config); err != nil {
        panic(err)
    }

    fmt.Printf("服务器配置: %+v\n", config.Server)
    fmt.Printf("数据库配置: %+v\n", config.DB)
}
```

### 4. 日志组件 (components/log)

**功能**: 结构化日志记录

**安装**:
```bash
go get github.com/apus-run/gala/components/log@v0.5.2
```

**使用**:
```go
package main

import (
    "context"

    "github.com/apus-run/gala/components/log"
)

func main() {
    logger := log.New()

    // 基本日志
    logger.Info("启动应用")

    // 带上下文的日志
    ctx := context.Background()
    logger.WithContext(ctx).Info("处理请求", log.Fields{
        "user_id": 123,
        "action":  "login",
    })

    // 错误日志
    if err := doSomething(); err != nil {
        logger.Error("操作失败", err)
    }
}

func doSomething() error {
    return nil
}
```

### 5. 认证组件 (components/authn)

**功能**: 用户认证

**安装**:
```bash
go get github.com/apus-run/gala/components/authn@v0.5.2
```

**使用**:
```go
package main

import (
    "context"

    "github.com/apus-run/gala/components/authn"
)

func main() {
    auth := authn.New()

    ctx := context.Background()

    // 用户登录
    token, err := auth.Login(ctx, "username", "password")
    if err != nil {
        panic(err)
    }

    // 验证令牌
    user, err := auth.Verify(ctx, token)
    if err != nil {
        panic(err)
    }

    println("用户:", user.Name)
}
```

### 6. 缓存组件 (components/cache)

**功能**: 内存缓存

**安装**:
```bash
go get github.com/apus-run/gala/components/cache@v0.5.2
```

**使用**:
```go
package main

import (
    "fmt"
    "time"

    "github.com/apus-run/gala/components/cache"
)

func main() {
    c := cache.New()

    // 设置缓存
    c.Set("key1", "value1", time.Minute)

    // 获取缓存
    val, ok := c.Get("key1")
    if ok {
        fmt.Println("缓存值:", val)
    }

    // 删除缓存
    c.Delete("key1")
}
```

### 7. JWT 工具 (pkg/jwtx)

**功能**: JWT 令牌生成和验证

**安装**:
```bash
go get github.com/apus-run/gala/pkg/jwtx@v0.5.2
```

**使用**:
```go
package main

import (
    "time"

    "github.com/apus-run/gala/pkg/jwtx"
)

func main() {
    // 生成令牌
    token, err := jwtx.Generate(jwtx.Claims{
        Subject:   "user123",
        ExpiresAt: time.Now().Add(time.Hour).Unix(),
    }, "secret-key")
    if err != nil {
        panic(err)
    }

    // 验证令牌
    claims, err := jwtx.Parse(token, "secret-key")
    if err != nil {
        panic(err)
    }

    println("用户:", claims.Subject)
}
```

### 8. 分布式锁 (components/dlock)

**功能**: 分布式锁实现

**安装**:
```bash
go get github.com/apus-run/gala/components/dlock@v0.5.2
```

**使用**:
```go
package main

import (
    "time"

    "github.com/apus-run/gala/components/dlock"
)

func main() {
    lock := dlock.NewRedisLock("redis://localhost:6379")

    // 获取锁
    ok, err := lock.Acquire("resource-key", time.Second*10)
    if err != nil || !ok {
        panic("获取锁失败")
    }
    defer lock.Release("resource-key")

    // 执行需要锁保护的操作
    doProtectedWork()
}

func doProtectedWork() {
    // ...
}
```

### 9. gRPC 扩展 (components/grpcx)

**功能**: gRPC 工具和中间件

**安装**:
```bash
go get github.com/apus-run/gala/components/grpcx@v0.5.2
```

**使用**:
```go
package main

import (
    "google.golang.org/grpc"

    "github.com/apus-run/gala/components/grpcx"
)

func main() {
    // 创建 gRPC 连接
    conn, err := grpc.Dial("localhost:8080", grpc.WithInsecure())
    if err != nil {
        panic(err)
    }
    defer conn.Close()

    // 使用拦截器
    client := grpcx.NewClient(conn)
    client.UseUnary(grpcx.LoggingInterceptor)
    client.UseStream(grpcx.LoggingInterceptor)
}
```

### 10. WebSocket (components/ws)

**功能**: WebSocket 连接管理

**安装**:
```bash
go get github.com/apus-run/gala/components/ws@v0.5.2
```

**使用**:
```go
package main

import (
    "github.com/apus-run/gala/components/ws"
)

func main() {
    hub := ws.NewHub()

    // 启动 WebSocket 服务
    hub.HandleFunc("/ws", func(conn *ws.Conn) {
        for {
            msg, err := conn.ReadMessage()
            if err != nil {
                break
            }
            conn.WriteMessage(msg)
        }
    })

    hub.Start(":8080")
}
```

## 本地开发

### 使用 go.work

如果需要同时修改多个模块：

```bash
# 1. 初始化工作空间
go work init

# 2. 添加模块到工作空间
go work use .
go work use ./components/db
go work use ./components/cache
go work use ./pkg/errorsx

# 3. 同步依赖
go work sync

# 4. 现在可以在项目中直接引用本地代码
# go.mod 会自动使用本地版本
```

**注意**: `go.work` 只用于本地开发，完成后请删除或添加到 `.gitignore`。

### 使用 replace 指令

在 `go.mod` 中临时替换模块路径：

```go
module myproject

go 1.25

require (
    github.com/apus-run/gala/components/db v0.5.2
)

// 本地开发时替换为本地路径
replace github.com/apus-run/gala/components/db => /path/to/gala/components/db
```

## 最佳实践

### 1. 依赖管理

```bash
# 添加依赖时明确指定版本
go get github.com/apus-run/gala/components/db@v0.5.2

# 更新到最新版本
go get github.com/apus-run/gala/components/db@latest

# 清理和验证依赖
go mod tidy
go mod verify
```

### 2. 错误处理

```go
// 始终使用 errorsx 处理错误
if err := doSomething(); err != nil {
    return errorsx.Wrap(err, "操作失败")
}
```

### 3. 配置管理

```go
// 使用强类型配置结构体
type AppConfig struct {
    DB   db.Config   `json:"db"`
    Cache cache.Config `json:"cache"`
    Log  log.Config  `json:"log"`
}
```

### 4. 日志记录

```go
// 始终包含上下文信息
logger.WithContext(ctx).Info("操作完成", log.Fields{
    "user_id": user.ID,
    "action":  "create",
})
```

## 故障排除

### 问题: 伪版本 (v0.0.0-xxx)

**原因**: 未指定版本号

**解决**:
```bash
go get github.com/apus-run/gala/components/db@v0.5.2
```

### 问题: 模块未找到

**原因**: 标签未推送或版本不存在

**解决**:
```bash
# 检查远程标签
git ls-remote --tags origin

# 更新模块缓存
go clean -modcache
go mod download
```

### 问题: go.work 冲突

**原因**: 在项目根目录使用了 go.work

**解决**:
```bash
# 删除工作空间文件
rm go.work go.work.sum

# 使用 replace 指令替代
```

## 更多资源

- [版本管理策略](VERSIONING.md)
- [Go Modules 文档](https://golang.org/ref/mod)
- [项目主页](https://github.com/apus-run/gala)
