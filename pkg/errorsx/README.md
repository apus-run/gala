# errorsx

[![Go Report Card](https://goreportcard.com/badge/github.com/apus-run/gala/pkg/errorsx)](https://goreportcard.com/report/github.com/apus-run/gala/pkg/errorsx)
[![Go Reference](https://pkg.go.dev/badge/github.com/apus-run/gala/pkg/errorsx.svg)](https://pkg.go.dev/github.com/apus-run/gala/pkg/errorsx)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

一个符合 [Google AIP-193](https://google.aip.dev/193) 标准的 Go 错误处理包，提供结构化错误信息和 gRPC 集成支持。

## ✨ 特性

- ✅ **符合 AIP-193 标准**：严格遵循 Google API 错误处理规范
- ✅ **gRPC 集成**：自动生成 ErrorInfo 详情，支持 LocalizedMessage 和 Help
- ✅ **结构化错误**：清晰的 Code、Status、Message、Details 字段
- ✅ **错误链支持**：支持 cause 和 stack 跟踪
- ✅ **预定义错误类型**：常用 HTTP 和业务错误类型
- ✅ **类型安全**：完整的 Go 类型检查
- ✅ **向后兼容**：保持 JSON 序列化格式稳定

## 🚀 快速开始

### 安装

```bash
go get github.com/apus-run/gala/pkg/errorsx
```

### 基础用法

```go
package main

import (
    "fmt"
    "github.com/apus-run/gala/pkg/errorsx"
)

func main() {
    // 创建简单错误
    err := errorsx.New(404, "USER_NOT_FOUND").
        WithMessage("User not found")

    fmt.Println(err)
    // Output: error: code = 404, status = USER_NOT_FOUND, message = User not found

    // 创建带详情的错误
    err = errorsx.New(400, "INVALID_PARAMS").
        WithMessage("Invalid request parameters").
        WithDetails(map[string]string{
            "field": "username",
            "reason": "does not match pattern",
        })

    fmt.Printf("Code: %d, Status: %s\n", err.Code, err.Status)
    // Output: Code: 400, Status: INVALID_PARAMS
}
```

## 📖 详细用法

### 1. 创建错误

#### 使用 New() 函数

```go
err := errorsx.New(500, "INTERNAL_ERROR").
    WithMessage("Something went wrong")
```

#### 使用预定义错误类型

```go
// 客户端错误
err := errorsx.NotFound("RESOURCE_NOT_FOUND").
    WithMessage("The requested resource was not found")

// 服务器错误
err = errorsx.InternalServer("DATABASE_ERROR").
    WithMessage("Database connection failed")

// 业务错误
err = errorsx.InvalidParams("VALIDATION_FAILED").
    WithMessage("Invalid input parameters")
```

### 2. 设置详情信息

```go
err := errorsx.New(429, "RATE_LIMITED").
    WithMessage("Too many requests").
    WithDetails(map[string]string{
        "reset_time": "2024-01-01T00:00:00Z",
        "limit": "1000/hour",
    })

// 或者使用 KV 方法
err = errorsx.New(400, "VALIDATION_ERROR").
    WithMessage("Validation failed").
    KV("field", "email").
    KV("value", "invalid-email").
    KV("pattern", "^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$")
```

### 3. 错误链和堆栈

```go
func doSomething() error {
    err := errorsx.New(500, "OPERATION_FAILED").
        WithMessage("Operation failed")

    // 添加原始错误
    if cause != nil {
        err = err.WithCause(cause)
    }

    // 添加堆栈信息
    return err.WithStack()
}
```

### 4. gRPC 集成

```go
// 创建错误
err := errorsx.New(404, "USER_NOT_FOUND").
    WithMessage("User not found").
    WithDetails(map[string]string{
        "user_id": "12345",
    })

// 转换为 gRPC 状态（自动包含 ErrorInfo）
st := err.GRPCStatus()
fmt.Printf("gRPC Code: %s\n", st.Code())
fmt.Printf("gRPC Message: %s\n", st.Message())
fmt.Printf("Details: %+v\n", st.Details())
```

### 5. 错误检查

```go
err := errorsx.NotFound("RESOURCE_NOT_FOUND")

// 使用 IsXxx 函数检查错误类型
if errorsx.IsNotFound(err) {
    // 处理资源未找到错误
}

// 检查错误代码
if errorsx.Code(err) == 404 {
    // 处理 404 错误
}

// 获取错误状态
status := errorsx.GetStatus(err)
if status == "RESOURCE_NOT_FOUND" {
    // 处理特定状态
}
```

### 6. 错误转换

```go
// 从标准 error 转换
stdErr := errors.New("something went wrong")
err := errorsx.FromError(stdErr)

// 克隆错误
clonedErr := err.Clone()

// 错误解包
if unwrapped := errors.Unwrap(err); unwrapped != nil {
    fmt.Printf("Original error: %v\n", unwrapped)
}
```

## 🔧 预定义错误类型

### 客户端错误

| 函数 | HTTP Code | 默认消息 | 用途 |
|------|-----------|----------|------|
| `BadRequest` | 400 | Bad Request | 请求参数错误 |
| `Unauthorized` | 401 | Unauthorized | 未认证 |
| `Forbidden` | 403 | Forbidden | 无权限 |
| `NotFound` | 404 | Not Found | 资源未找到 |
| `Conflict` | 409 | Conflict | 资源冲突 |

### 服务器错误

| 函数 | HTTP Code | 默认消息 | 用途 |
|------|-----------|----------|------|
| `InternalServer` | 500 | Internal Server Error | 内部服务器错误 |
| `ServiceUnavailable` | 503 | Service Unavailable | 服务不可用 |
| `GatewayTimeout` | 504 | Gateway Timeout | 网关超时 |

### 业务错误

| 函数 | HTTP Code | 默认消息 | 用途 |
|------|-----------|----------|------|
| `InvalidParams` | 400 | Invalid Params | 参数验证失败 |
| `InvalidArguments` | 400 | Invalid Arguments | 参数错误 |
| `BindError` | 400 | Bind Error | 数据绑定错误 |
| `DBReadError` | 500 | DB Read Error | 数据库读错误 |
| `DBWriteError` | 500 | DB Write Error | 数据库写错误 |
| `DBTransactionError` | 500 | DB Transaction Error | 数据库事务错误 |

### 认证相关

| 函数 | HTTP Code | 默认消息 | 用途 |
|------|-----------|----------|------|
| `TokenInvalid` | 401 | Token Invalid | Token 无效 |
| `TokenExpired` | 401 | Token Expired | Token 过期 |
| `TokenInvalidSignature` | 401 | Token Invalid Signature | Token 签名无效 |
| `PermissionDenied` | 403 | Permission Denied | 权限拒绝 |

## 🎯 最佳实践

### 1. 命名规范

**Status 字段**应遵循 UPPER_SNAKE_CASE 命名规范：

```go
✅ 正确
errorsx.New(404, "USER_NOT_FOUND")
errorsx.New(400, "INVALID_PARAMETERS")
errorsx.New(500, "DATABASE_CONNECTION_FAILED")

❌ 错误
errorsx.New(404, "UserNotFound")
errorsx.New(400, "invalid_parameters")
errorsx.New(500, "DbError")
```

### 2. Message 编写

Message 应简洁、可操作，并避免技术术语：

```go
✅ 好的消息
"User not found"
"Invalid email format"
"Database connection failed, please try again later"

❌ 不好的消息
"An error occurred"
"Something went wrong"
"ERROR_500_INTERNAL_SERVER_ERROR"
```

### 3. Details 使用

Details 用于提供机器可读的上下文信息：

```go
✅ 推荐用法
err := errorsx.NotFound("USER_NOT_FOUND").
    WithMessage("User not found").
    WithDetails(map[string]string{
        "user_id": "12345",
        "operation": "get_user",
    })

// 便于客户端程序化处理
if details, ok := err.Details.(map[string]string); ok {
    userID := details["user_id"]
    // 使用 userID 进行后续处理
}
```

### 4. 错误处理

```go
// 在业务逻辑中
func GetUser(id string) (*User, error) {
    user, err := db.GetUser(id)
    if err != nil {
        // 添加上下文信息但不暴露内部错误
        return nil, errorsx.NotFound("USER_NOT_FOUND").
            WithMessage("User not found").
            WithCause(err).
            WithStack().
            WithDetails(map[string]string{
                "user_id": id,
            })
    }
    return user, nil
}

// 在 HTTP 处理器中
func handleGetUser(w http.ResponseWriter, r *http.Request) {
    user, err := GetUser(userID)
    if err != nil {
        // 日志记录包含完整错误信息
        log.Printf("Error: %v", err)

        // 返回给客户端的结构化错误
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(errorsx.Code(err))
        json.NewEncoder(w).Encode(err)
        return
    }
    // ...
}
```

## 📊 字段说明

| 字段 | 类型 | 说明 | JSON 标签 |
|------|------|------|-----------|
| `Code` | `int` | HTTP 状态码 | `code` |
| `Status` | `string` | 业务错误码（UPPER_SNAKE_CASE） | `status` |
| `Message` | `string` | 人类可读的错误消息 | `message` |
| `Details` | `any` | 机器可读的详细信息 | `details` |
| `cause` | `error` | 原始错误（私有字段） | - |
| `stack` | `[]string` | 调用堆栈（私有字段） | - |

## Gin ErrorBuilder 中间件

```go
type Builder struct{}

func NewBuilder() *Builder {
	return &Builder{}
}

func (b *Builder) Build() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		// 下游已经写回响应时，避免重复写入。
		if c.Writer.Written() {
			return
		}

		if len(c.Errors) == 0 {
			return
		}

		appErr := errorsx.FromError(c.Errors.Last().Err)
		if appErr == nil {
			return
		}

		if appErr.Code < 100 || appErr.Code > 599 {
			appErr.Code = http.StatusInternalServerError
		}

		log.Printf("[GIN][ERROR] %+v", appErr)

		resp := gin.H{
			"code":    appErr.Code,
			"status":  appErr.Status,
			"message": appErr.Message,
		}
		if appErr.Details != nil {
			resp["details"] = appErr.Details
		}

		c.AbortWithStatusJSON(appErr.Code, resp)
	}
}

```

## 🔗 相关资源

- [Google AIP-193 错误处理标准](https://google.aip.dev/193)
- [gRPC 错误详情](https://github.com/googleapis/googleapis/blob/master/google/rpc/error_details.proto)
- [Go 错误处理最佳实践](https://go.dev/blog/go1.13-errors)

## 🤝 贡献

欢迎提交 Issue 和 Pull Request！

### 开发指南

1. Fork 项目
2. 创建特性分支 (`git checkout -b feature/AmazingFeature`)
3. 提交更改 (`git commit -m 'feat: add some AmazingFeature'`)
4. 推送到分支 (`git push origin feature/AmazingFeature`)
5. 打开 Pull Request

### 运行测试

```bash
go test -v -count=1 ./...
go fmt ./...
go vet ./...
```

## 📝 许可证

本项目采用 MIT 许可证 - 查看 [LICENSE](LICENSE) 文件了解详情。

## 📞 联系方式

- 项目主页: [https://github.com/apus-run/gala](https://github.com/apus-run/gala)
- 问题追踪: [https://github.com/apus-run/gala/issues](https://github.com/apus-run/gala/issues)

---

<div align="center">
  <strong>Made with ❤️ by the Gala Team</strong>
</div>
