# errcode 使用手册

`errcode` 用于 Go HTTP 服务的业务错误处理。业务层可以返回稳定的错误标识并保留底层原因，HTTP 层负责记录日志、选择状态码和生成客户端响应。模块要求 Go 1.27，仅依赖标准库。

```go
import "github.com/apus-run/gala/pkg/errcode"
```

在独立项目中联调 Gala 工作树时，可在该项目的 `go.mod` 中配置本地依赖，将路径替换为实际目录。

```go.mod
require github.com/apus-run/gala/pkg/errcode v0.0.0

replace github.com/apus-run/gala/pkg/errcode => /path/to/gala/pkg/errcode
```

## 快速接入 net/http

下面是一个完整的用户存在性查询服务。示例中只有用户 `1` 存在。处理函数在成功时写入 204 并返回 `nil`，在失败时将错误交给 `AppHandler`。所有可能返回业务错误的检查都在提交响应之前完成。

```go
package main

import (
	"log"
	"net/http"

	"github.com/apus-run/gala/pkg/errcode"
)

var ErrUserNotFound = errcode.New(http.StatusNotFound, "NotFound.User", "用户不存在")

func checkUser(w http.ResponseWriter, r *http.Request) *errcode.AppError {
	id := r.PathValue("id")
	if id != "1" {
		return ErrUserNotFound.KV("id", id)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func main() {
	mux := http.NewServeMux()
	mux.Handle("GET /users/{id}/exists", errcode.AppHandler(checkUser))
	log.Fatal(http.ListenAndServe("127.0.0.1:8080", mux))
}
```

将示例保存为 `main.go`，运行 `go run .`，在另一终端执行 `curl -i http://127.0.0.1:8080/users/7/exists`。应收到 HTTP 404，媒体类型为 `application/problem+json`，正文如下。

```json
{
  "title": "Not Found",
  "status": 404,
  "detail": "用户不存在",
  "reason": "NotFound.User",
  "metadata": {"id": "7"}
}
```

## 业务实战：买家取消订单

买家在订单页点击“取消”时，支付回调可能刚把订单改为已支付。此时前端要刷新订单并展示退款入口。如果取消已经成功，只是第一次响应丢失，买家重试应当看到成功。

接口为 `POST /orders/{id}/cancel`。示例使用 SQLite，`orders` 表包含主键 `id`、买家标识 `buyer_id` 和 `status`，状态限定为 `pending`、`paid`、`cancelled`。买家身份取自认证中间件写入的请求上下文。

| 取消时遇到的情况 | 响应 | 订单页的动作 |
| --- | --- | --- |
| 待支付，或同一买家重复取消 | 204，无正文 | 将订单显示为已取消 |
| 支付已完成 | 409，`Order.AlreadyPaid` | 刷新订单，展示退款入口 |
| 订单不存在，或属于其他买家 | 404，`Order.NotFound` | 提示订单不可用，返回列表 |
| 登录凭证缺失 | 401，`Unauthenticated` | 引导登录 |
| 数据库查询失败 | 未分类故障为 500，`InternalError` | 保留当前页面并允许重试；服务端日志记录失败操作 |

**订单服务。** 取消和支付都应使用带 `status = 'pending'` 条件的更新，让数据库决定谁先完成状态转换。下面的取消操作只在更新未命中时查询原因。两条 SQL 都按买家过滤，以免向其他买家暴露订单状态。

```go
var (
	ErrOrderNotFound = errcode.New(http.StatusNotFound, "Order.NotFound", "订单不存在")
	ErrAlreadyPaid   = errcode.New(http.StatusConflict, "Order.AlreadyPaid", "订单已支付，请申请退款")
)

func cancelOrder(ctx context.Context, db *sql.DB, buyerID, orderID string) error {
	var id string
	err := db.QueryRowContext(ctx, `
		UPDATE orders SET status = 'cancelled'
		WHERE id = ? AND buyer_id = ? AND status = 'pending'
		RETURNING id`, orderID, buyerID).Scan(&id)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("取消订单 %s: %w", orderID, err)
	}

	var status string
	err = db.QueryRowContext(ctx,
		"SELECT status FROM orders WHERE id = ? AND buyer_id = ?",
		orderID, buyerID).Scan(&status)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return ErrOrderNotFound.Wrap(err)
	case err != nil:
		return fmt.Errorf("读取订单 %s 的取消结果: %w", orderID, err)
	case status == "cancelled":
		return nil
	case status == "paid":
		return ErrAlreadyPaid.KV("order_id", orderID)
	default:
		return fmt.Errorf("订单 %s 取消时状态异常: %q", orderID, status)
	}
}
```

示例只更新订单状态。如果取消还要释放同库中的预留库存，应将库存释放和首次成功的状态更新放在同一事务中，重复取消直接返回成功。

**HTTP 出口。** 注册路由时传入数据库连接和已有的身份读取函数。订单服务返回的普通错误在这里交给 `Normalize`，业务错误的 Reason 和底层错误链都会保留。

```go
func cancelHandler(db *sql.DB, currentBuyer func(context.Context) (string, bool)) errcode.AppHandler {
	return func(w http.ResponseWriter, r *http.Request) *errcode.AppError {
		buyerID, ok := currentBuyer(r.Context())
		if !ok {
			return errcode.ErrUnauthenticated
		}
		if err := cancelOrder(r.Context(), db, buyerID, r.PathValue("id")); err != nil {
			return errcode.Normalize(err)
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
}

// 在路由初始化时注册：
// mux.Handle("POST /orders/{id}/cancel", cancelHandler(db, currentBuyer))
```

例如买家取消已支付的订单 `A1024`，客户端收到 HTTP 409 和下面的正文。订单页按 `reason` 选择退款流程，`detail` 可直接展示给买家。

```json
{
  "title": "Conflict",
  "status": 409,
  "detail": "订单已支付，请申请退款",
  "reason": "Order.AlreadyPaid",
  "metadata": {"order_id": "A1024"}
}
```

## Gin 实战：封装订单列表响应

在 Gin 应用的 `app/response.go` 中定义响应包装器，控制器通过 `NewResponse(c)` 写入响应。示例使用 Gin v1.12.0。[Gin 渲染与绑定文档](https://github.com/gin-gonic/gin/blob/v1.12.0/docs/doc.md)

这组路由统一返回 `application/json`。成功时 `code` 为 0，失败时 `code` 取实际 HTTP 状态，客户端按 `reason` 区分业务错误。

```go
package app

import (
	"log/slog"
	"net/http"

	"github.com/apus-run/gala/pkg/errcode"
	"github.com/gin-gonic/gin"
)

type Pagination struct {
	Page      int   `json:"page"`
	PageSize  int   `json:"page_size"`
	TotalRows int64 `json:"total_rows"`
}

// Result 是 Gin 接口返回给客户端的 JSON 响应体。
type Result struct {
	Code       int               `json:"code"`
	Msg        string            `json:"msg"`
	RequestID  string            `json:"request_id"`
	Data       any               `json:"data"`
	Pagination *Pagination       `json:"pagination,omitempty"`
	Reason     string            `json:"reason,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

type Response struct {
	ctx *gin.Context
}

func NewResponse(c *gin.Context) *Response { return &Response{ctx: c} }

func (r *Response) Success(data any) {
	r.writeSuccess(data, nil)
}

func (r *Response) SuccessPage(data any, page Pagination) {
	r.writeSuccess(data, &page)
}

func (r *Response) writeSuccess(data any, page *Pagination) {
	r.ctx.JSON(http.StatusOK, Result{
		Msg: "success", RequestID: r.ctx.GetString("traceid"),
		Data: data, Pagination: page,
	})
}

func (r *Response) SuccessOk() { r.Success(nil) }

func (r *Response) Error(err error) {
	e := errcode.Normalize(err)
	if e == nil {
		return
	}
	id := r.ctx.GetString("traceid")
	status, body := errcode.Respond(e)
	level := slog.LevelWarn // 4xx 是客户端可预期的失败，只有 5xx 记为 Error
	if status >= http.StatusInternalServerError {
		level = slog.LevelError
	}
	slog.Log(r.ctx.Request.Context(), level, "request failed", "request_id", id, "err", e)
	r.ctx.AbortWithStatusJSON(status, Result{
		Code: status, Msg: body.Detail, RequestID: id,
		Reason: body.Reason, Metadata: body.Metadata,
	})
}
```

每次请求创建一个 `Response`。详情接口调用 `Success(order)`，分页接口调用 `SuccessPage(orders, page)`，将列表和本次查询的分页信息一起写入响应。请求 ID 中间件用 `c.Set("traceid", id)` 放入字符串，日志和响应就能用同一 ID 关联。错误出口从 `Respond` 取公开文案，底层原因随 `AppError` 写入日志。

响应始终包含 `data` 字段。`SuccessOk()` 返回 HTTP 200 和 `"data": null`，失败响应同样使用 `null`。列表查询成功且没有记录时返回 `"data": []`。

**控制器示例。** 在同一应用包的 `app/orders.go` 中，用 `router.GET("/orders", ListOrders(service.ListOrders))` 注册路由。认证中间件将当前买家 ID 存入 `buyer_id`。服务方法按该买家查询，返回当前页订单和匹配的总条数。

```go
package app

import (
	"context"

	"github.com/apus-run/gala/pkg/errcode"
	"github.com/gin-gonic/gin"
)

type Order struct {
	ID string `json:"id"`
}

func ListOrders(list func(context.Context, string, int, int) ([]Order, int64, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		resp := NewResponse(c)
		buyer := c.GetString("buyer_id")
		if buyer == "" {
			resp.Error(errcode.ErrUnauthenticated)
			return
		}
		var q struct {
			Page int `form:"page,default=1" binding:"min=1"`
			Size int `form:"page_size,default=20" binding:"min=1,max=100"`
		}
		if err := c.ShouldBindQuery(&q); err != nil {
			resp.Error(errcode.ErrInvalidArgument.WithMessage("分页参数无效").Wrap(err))
			return
		}
		items, total, err := list(c.Request.Context(), buyer, q.Page, q.Size)
		if err != nil {
			resp.Error(err)
			return
		}
		if items == nil {
			items = []Order{}
		}
		resp.SuccessPage(items, Pagination{Page: q.Page, PageSize: q.Size, TotalRows: total})
	}
}
```

假设当前买家只有一笔订单 `A1024`，请求 `GET /orders?page=1&page_size=20` 返回 HTTP 200：

```json
{"code":0,"msg":"success","request_id":"req-42","data":[{"id":"A1024"}],"pagination":{"page":1,"page_size":20,"total_rows":1}}
```

同一买家传入 `page_size=101` 时返回 HTTP 400，服务层不会收到查询：

```json
{"code":400,"msg":"分页参数无效","request_id":"req-43","data":null,"reason":"InvalidArgument"}
```

## 定义业务错误

在拥有该业务语义的包中，用 `New(status, reason, message)` 定义可复用的错误。客户端用 `reason` 判断业务分支，用 `detail` 展示文案。例如缺少凭证时可返回 `ErrUnauthenticated`；如果凭证过期需要触发刷新，认证包可以定义 `New(http.StatusUnauthorized, "Auth.TokenExpired", "登录已过期")`。两种情况都返回 HTTP 401。

| 方法 | 用途 |
| --- | --- |
| `Code()` / `Reason()` | 读取声明的 HTTP 状态和稳定业务标识，两者共同决定错误类别 |
| `HTTPStatus()` | 读取最终响应状态。非 nil 错误的声明值在 400–599 范围外时返回 500，nil 接收者返回 200 |
| `Message()` / `WithMessage(text)` | 读取或替换当前实例的文案 |
| `KV(key, value)` / `Metadata()` | 添加可公开的字符串键值，或读取元数据副本。同名键取最后一次设置的值 |
| `Wrap(cause)` / `Unwrap()` | 替换或读取底层原因。`Wrap(nil)` 清除副本上的原因 |
| `WithStack()` / `Stack()` | 捕获或读取调用栈副本；已有栈时 `WithStack` 原样返回 |

`WithMessage`、`KV` 和 `Wrap` 都返回副本。请求可以从同一个预定义错误派生出各自的实例，并发复用时应避免重新赋值共享变量。`Wrap` 附加非 nil 原因时在首次派生的副本上捕获调用栈（5 层），供日志定位；栈不进入响应，HTTP 出口的 `Normalize` 不再重复捕获。

### 预定义错误速查

| 错误 | HTTP | 客户端 reason |
| --- | ---: | --- |
| `ErrInvalidArgument` | 400 | `InvalidArgument` |
| `ErrUnauthenticated` | 401 | `Unauthenticated` |
| `ErrPermissionDenied` | 403 | `PermissionDenied` |
| `ErrNotFound` | 404 | `NotFound` |
| `ErrMethodNotAllowed` | 405 | `MethodNotAllowed` |
| `ErrConflict` | 409 | `Conflict` |
| `ErrTooManyRequests` | 429 | `TooManyRequests` |
| `ErrCanceled` | 499 | `Canceled` |
| `ErrInternal` | 500 | `InternalError` |
| `ErrServiceUnavailable` | 503 | `ServiceUnavailable` |
| `ErrTimeout` | 504 | `DeadlineExceeded` |

## 保留原因、识别错误与归一化

仓储层可以用 `fmt.Errorf("查询用户 %d: %w", id, err)` 为失败操作补充上下文。业务层决定查无记录的含义。读取用户详情时可以映射为 404，读取可选配置时则可以采用默认值。关于包装与错误链的背景，见 [Datadog 的 Go 错误处理指南](https://www.datadoghq.com/blog/go-error-handling/)。

| 调用意图 | 写法 |
| --- | --- |
| 判断错误链中是否包含某一业务错误或底层原因 | `errors.Is(err, ErrUserNotFound)` 或 `errors.Is(err, sql.ErrNoRows)` |
| 提取链中已有的业务错误，读取其字段 | 声明 `var e *errcode.AppError`，在 `errors.As(err, &e) && e != nil` 时读取 |
| 在 HTTP 出口为任意错误确定公开分类 | `e := errcode.Normalize(err, mappers...)` |

### 用 Mapper 翻译第三方错误

查询用户的接口可以把下面的函数传给 `errcode.Normalize(err, mapUserLookup)`。Mapper 接收完整的原始错误，匹配时返回业务错误，未匹配时返回 `nil` 交给下一条规则。框架会保留原始错误作为原因。

```go
func mapUserLookup(err error) *errcode.AppError {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrUserNotFound
	}
	return nil
}
```

`Normalize(nil)` 返回 nil 指针。其余输入按以下顺序分类，首个匹配决定公开字段。

1. 查找错误链中的第一个 `*AppError`。输入本身是有效的 `*AppError` 时原样返回。匹配到带类型的 nil 指针时归为 `ErrInternal`。
2. 按传入顺序调用非 nil 的 Mapper，采用第一个非 nil 结果。
3. 匹配到 `context.DeadlineExceeded`、`os.ErrDeadlineExceeded`，或错误链中第一个实现 `Timeout() bool` 的错误，且其 `Timeout()` 返回 true 时，归为 `ErrTimeout`。
4. 匹配到 `context.Canceled` 时归为 `ErrCanceled`，其余错误归为 `ErrInternal`。

对于包装错误和 `errors.Join`，归一化结果保留完整输入作为底层原因，其他分支仍可通过 `errors.Is` 或 `errors.As` 访问。返回类型为 `error` 的业务函数在成功时应直接返回 `nil`。把 `Normalize(nil)` 赋给 `error` 会得到包含 nil 指针的非 nil 接口。

## HTTP 响应与服务端日志

`AppHandler` 通过 `slog` 记录请求错误，再用 `Respond` 生成响应。HEAD 请求只发送响应头。错误响应带 `X-Content-Type-Options: nosniff` 和 `Cache-Control: no-store`，避免中间缓存复用携带每请求 metadata 的响应。使用其他 HTTP 适配器时，先调用 `Normalize`，在结果非 nil 时记录日志，再用 `status, body := errcode.Respond(e)` 获取状态与正文，并在写入前设置 `Content-Type`（`errcode.ContentType`）以及上述两个安全头。

**日志级别。** 响应状态为 5xx 时记为 `Error`，其余记为 `Warn`。5xx 是服务端故障，4xx 是客户端可预期的失败，不应与故障混在同一级别触发告警；OpenTelemetry 的 [HTTP 语义约定](https://opentelemetry.io/docs/specs/semconv/http/http-spans/)同样规定服务端 span 遇到 4xx 不置为错误。`AppHandler` 的日志包含请求方法和路径，不含查询串，其中可能带有凭证。响应写入失败只可能是连接已断开，`AppHandler` 与 `http.Error` 一样忽略。

**405 与 Allow 头。** RFC 9110 要求 405 响应必须带 `Allow` 头，列出资源当前支持的方法。`ErrMethodNotAllowed` 只表达分类，`Respond` 不生成该头，需要适配器在写响应前自行设置。

**客户端字段。** `Body` 使用 Problem Details 的 `title`、`status`、`detail`，并用 `reason`、`metadata` 承载业务信息。空字段在 JSON 中省略，例如 499 没有标准状态短语，响应会省略 `title`。`Respond(nil)` 返回 200 和 `Body{}`。协议格式见 [RFC 9457](https://www.rfc-editor.org/rfc/rfc9457.html)。

**内部故障。** 数据库或上游服务故障可通过 `errcode.ErrInternal.Wrap(err)` 返回。当前错误与 `ErrInternal` 的声明状态和 Reason 均相同时，响应的 `detail` 固定为 `Internal server error.`。即使通过 `WithMessage` 加入了诊断信息，这条规则也适用。独立定义的其他 500 类业务错误使用各自的文案。所有错误的 `KV` 元数据都可能返回客户端，只应放入可公开的值。零值 `AppError` 按 500 响应，`reason` 和 `detail` 回填 `ErrInternal` 的预定义值。

**日志字段。** `slog.Log(ctx, level, "request failed", "err", e)` 会通过 `LogValue()` 输出 `code`、`reason`、`message` 以及可选的 `metadata`、`cause`、`stack`。其中 `code` 为声明状态，`cause` 保留底层错误文本，`stack` 是业务侧 `Wrap` 或 `WithStack` 捕获的调用栈。`Error()` 也包含诊断细节，生成 HTTP 响应时应使用 `Respond` 返回的 `Body`。

## 本地验证

在 `pkg/errcode` 目录执行以下命令，可查看完整 API 并运行独立模块的检查。

```sh
GOWORK=off go doc -all .
GOWORK=off go vet ./...
GOWORK=off go test -race -shuffle=on ./...
```
