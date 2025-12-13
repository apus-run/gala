# 使用示例

## 安装依赖

```bash
go get github.com/apus-run/gala/components/db@components/db/v0.6.2
go get github.com/apus-run/gala/components/cache@components/cache/v0.6.2
go get github.com/apus-run/gala/pkg/errorsx@pkg/errorsx/v0.6.2
```

## 基本使用

### 数据库组件

```go
import "github.com/apus-run/gala/components/db"

type User struct {
    ID   int64  `json:"id"`
    Name string `json:"name"`
}

repo := db.NewRepository()
user := &User{Name: "张三"}

ctx := context.Background()
if err := repo.Create(ctx, user); err != nil {
    panic(err)
}
```

### 错误处理

```go
import "github.com/apus-run/gala/pkg/errorsx"

err := errorsx.New("业务错误")
err = errorsx.Wrap(err, "包装错误信息")

if errorsx.Is(err, "业务错误") {
    println("找到目标错误")
}
```

### 缓存组件

```go
import (
    "time"
    "github.com/apus-run/gala/components/cache"
)

c := cache.New()
c.Set("key1", "value1", time.Minute)

val, ok := c.Get("key1")
if ok {
    println(val)
}
```

### JWT 工具

```go
import "github.com/apus-run/gala/pkg/jwtx"

token, err := jwtx.Generate(jwtx.Claims{
    Subject:   "user123",
    ExpiresAt: time.Now().Add(time.Hour).Unix(),
}, "secret-key")

claims, err := jwtx.Parse(token, "secret-key")
```

## 本地开发

### go.work

```bash
go work init
go work use ./components/db
go work use ./pkg/errorsx
go work sync
```

### replace 指令

```go
// go.mod
replace github.com/apus-run/gala/components/db => /path/to/gala/components/db
```

## 最佳实践

1. **明确指定版本**
   ```bash
   go get github.com/apus-run/gala/components/db@components/db/v0.6.2
   ```

2. **错误处理**
   ```go
   if err := doSomething(); err != nil {
       return errorsx.Wrap(err, "操作失败")
   }
   ```

3. **统一版本**
   - 所有模块使用相同版本号
   - 例如：v0.6.2

---

**参考**:
- [VERSIONING.md](VERSIONING.md) - 版本管理策略
- [QUICK_START.md](QUICK_START.md) - 快速开始
