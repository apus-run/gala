# Logger

## 使用方式

### slog

本包基于标准库 `log/slog`。你依然可以直接使用原生的 slog handler。

```go
l := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
	Level: logger.LevelInfo,
}))

l.InfoContext(ctx, "agent started",
	"provider", "anthropic",
	"model", model,
)
```

### 全局 logger

为便于渐进式迁移，本包提供了常用的全局辅助函数。它们的签名与 slog 一致：
第一个参数是消息，其后是键值对或 `slog.Attr`。

```go
logger.SetDefault(logger.New(logger.WithLevel(logger.LevelDebug)))

logger.Info("started")
logger.Info("provider initialized", "provider", "anthropic", "model", model)
logger.InfoContext(ctx, "agent run started", "work_dir", workDir)
logger.Error("agent run failed", "err", err)
```

### Builder

`logger.NewHandler` 构建默认 handler。`logger.WrapHandler` 用 hermes-forge 的
logger 装饰器包装已有的 handler。`logger.NewLogger` 则是
`slog.New(logger.WrapHandler(...))` 的便捷封装。

下面是一个完整初始化示例：JSON 输出、敏感字段脱敏、服务元信息，以及
包级默认 logger：

```go
package main

import (
	"log/slog"
	"os"

	log "github.com/apus-run/gala/components/logger"
)

func initLogger(id, name, version string) {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
		ReplaceAttr: func(groups []string, attr slog.Attr) slog.Attr {
			if attr.Key == "password" || attr.Key == "token" {
				return slog.String(attr.Key, "***")
			}
			return attr
		},
	})

	logger := log.NewLogger(handler).With(
		slog.String("service.id", id),
		slog.String("service.name", name),
		slog.String("service.version", version),
	)

	log.SetDefault(logger)
}
```

调用 `SetDefault` 之后，包级辅助函数就会使用配置好的默认 logger：

```go
log.Info("service started")
log.Info("login",
	slog.String("user", userID),
	slog.String("password", password),
	slog.String("token", token),
)
```

输出是 JSON，每条记录都会带上服务字段，并把 `password` 和 `token` 的值
脱敏为 `"***"`。

```go
l := logger.NewLogger(
	slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logger.LevelInfo,
	}),
).With(
	slog.String("service.name", "hermes-forge"),
	slog.String("service.version", version),
)

logger.SetDefault(l)
```

常见场景下可以直接用 `logger.New`：

```go
l := logger.New(
	logger.WithWriter(os.Stdout),
	logger.WithFormat(logger.FormatJSON),
	logger.WithLevel(logger.LevelInfo),
	logger.WithReplaceAttr(func(groups []string, attr slog.Attr) slog.Attr {
		if attr.Key == "password" || attr.Key == "token" {
			return slog.String(attr.Key, "***")
		}
		return attr
	}),
	logger.WithAddSource(true),
)
```

可用选项：

`New` 与 `NewHandler` 的 handler 选项：

- `WithWriter(io.Writer)` 设置输出目标。默认：`os.Stderr`。
- `WithFormat(logger.FormatText | logger.FormatJSON | logger.FormatPretty)` 选择输出格式。
- `WithColor(bool)` 为 Pretty 的属性值启用 ANSI 颜色。默认：false。
- `WithLevel(logger.Leveler)` 设置最低启用级别。
- `WithAddSource(bool)` 是否带上调用方的源码位置。
- `WithReplaceAttr(func(groups []string, attr slog.Attr) slog.Attr)` 自定义属性。

`WrapHandler` 与 `NewLogger` 的 wrapper 选项：

- `WithExtractor(...logger.Extractor)` 添加上下文属性提取器。

handler 选项用于配置本包创建的 handler，wrapper 选项用于装饰已有的 handler。
这样一来，`NewLogger(existingHandler, ...)` 就不会假装自己能修改一个已经
创建好的 handler 的格式、输出目标或级别。

### Pretty 输出

本地调试时使用 `FormatPretty`。它保留标准 slog 的文本头部，并用
[pp/v3](https://github.com/k0kubun/pp) 展开属性值，包括结构体、map 和切片。
每条记录都会先缓冲，再由派生 logger 共享的锁保护写出，因此多行记录不会
相互交错。

```go
l := logger.New(
	logger.WithFormat(logger.FormatPretty),
	logger.WithColor(true), // 可选；默认不开启颜色。
)
ctx = logger.ContextWithAttrs(ctx, slog.String("request_id", "r1"))
l.InfoContext(ctx, "loaded", "roles", []string{"admin", "editor"})
```

不开启颜色时，输出大致如下：

```text
time=2026-10-13T10:00:00.000+08:00 level=INFO msg=loaded
roles => []string{
  "admin",
  "editor",
}
request_id => "r1"
```

`With`、`WithGroup`、上下文提取器、级别过滤、调用方源码位置和
`WithReplaceAttr` 都与 Pretty 输出兼容。值会先经过 `LogValuer` 解析，再
进行替换和格式化。分组键使用点分隔路径，例如 `request.user.id`。
`WithColor` 仅对 Pretty 输出生效。

Pretty 输出使用 Go 的值格式化，面向人工查看。对接日志采集器时请使用
`FormatJSON`。只有导出（首字母大写）的结构体字段会被展开。实现了
`encoding.TextMarshaler`、`fmt.Formatter`、`error` 或 `fmt.Stringer` 的
属性值，会使用其显式文本表示，而不是反射展开其字段。文本序列化失败会以
`!ERROR:` 值输出。

这些接口以及 `LogValuer` 只在属性边界被识别；对于结构体或 map 内部的字段，
pp 不会递归调用它们。若要脱敏嵌套数据，请在包含该数据的值上使用
`LogValuer`，或者直接替换整个属性。`ReplaceAttr` 作用于 slog 属性
（包括嵌套的 slog group），而不是任意的 Go 字段。

### 上下文属性

把属性附加到 `context.Context` 上，它们就会自动流经任何感知上下文的日志调用。

```go
ctx = logger.ContextWithAttrs(ctx,
	slog.String("trace_id", traceID),
	slog.String("chat_id", chatID),
)

logger.InfoContext(ctx, "handling message")
```

`logger.New`、`logger.NewHandler`、`logger.WrapHandler` 和 `logger.NewLogger`
默认都会附加 `logger.AttrsFromContext`。如果属性存放在上下文中的其它位置，
可以添加自定义提取器。

```go
l := logger.NewLogger(logger.NewHandler(), logger.WithExtractor(func(ctx context.Context) []slog.Attr {
	runID, _ := ctx.Value(runIDKey{}).(string)
	if runID == "" {
		return nil
	}
	return []slog.Attr{slog.String("run_id", runID)}
}))
```

### 替换属性

用 `WithReplaceAttr` 做脱敏或输出规范化。它会直接传给
`slog.HandlerOptions.ReplaceAttr`。

```go
l := logger.New(logger.WithReplaceAttr(func(groups []string, attr slog.Attr) slog.Attr {
	if attr.Key == "password" || attr.Key == "token" {
		return slog.String(attr.Key, "***")
	}
	return attr
}))
```

### 日志级别

本包为 slog 的级别起了别名，并新增了 `LevelFatal`。

```go
logger.LevelDebug
logger.LevelInfo
logger.LevelWarn
logger.LevelError
logger.LevelFatal
```

```go
level := logger.ParseLevel("debug")
```

无法识别的字符串会回退为 `LevelInfo`。

### 测试

```sh
go test ./logger
go test ./...
```
