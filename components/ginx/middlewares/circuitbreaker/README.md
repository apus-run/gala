# Gin Circuit Breaker

基于 `gofiber/contrib/v3/circuitbreaker` 直接移植的 Gin 熔断中间件。请求持续失败时，熔断器会暂时拒绝新请求；等待恢复时间后，通过有限的探测请求判断是否恢复。

本文按上游 README 的工作原理、配置、运维控制和 8 类使用场景组织，示例使用 Gin。移植保留上游 [MIT License](LICENSE)。

## Go 版本与安装

当前 ginx 模块要求 **Go 1.25 或更高版本**，本包使用 Gin，不依赖 Fiber。

```sh
go get github.com/apus-run/gala/components/ginx
```

导入：

```go
import "github.com/apus-run/gala/components/ginx/middlewares/circuitbreaker"
```

示例对应当前仓库源码。使用尚未发布的新增功能时，在应用的 `go.mod` 中通过 `replace` 指向本地 `components/ginx` 模块。

## 工作原理

1. **Closed：闭合。** 请求正常通过；失败计数达到 `FailureThreshold` 后进入 Open。闭合状态不限制业务并发。
2. **Open：打开。** 新请求立即被拒绝。`Timeout` 到期后，由下一次请求或状态查询触发 Half-Open，没有后台定时任务。没有请求或查询时，内部状态不会主动转换。
3. **Half-Open：半开。** 最多允许 `HalfOpenMaxConcurrent` 个探测同时执行；其它请求由 `OnHalfOpen` 响应。当前半开窗口的成功探测达到 `SuccessThreshold` 后闭合；其中一次失败就重新打开。

每次状态转换都会更新代际标识。旧请求迟到的成功、失败或名额释放不会改变新的状态窗口，避免旧结果干扰恢复。

熔断用于减少对故障服务的持续调用、在故障期间快速返回并控制恢复探测。下游执行超时和入口限流仍由各自组件负责。

## 接口签名

```go
func New(config Config) *CircuitBreaker
func Middleware(cb *CircuitBreaker) gin.HandlerFunc
func (cb *CircuitBreaker) Build() gin.HandlerFunc
```

先通过 `New` 创建实例，再挂载 `Middleware(cb)` 或等价的 `cb.Build()`。中间件统一完成入场、运行 Gin 处理链、报告结果和释放半开名额。

`AllowRequest`、`ReleaseSemaphore`、`ReportSuccess`、`ReportFailure` 保留上游接口，但已废弃。手动入场取得的半开名额会一直占用到手动释放或下一次状态转换；旧协议也不能把结果准确绑定到原来的状态窗口。应用应使用 Middleware/Build。

## 配置

| 属性 | 类型 | 说明 | 默认值 |
| --- | --- | --- | --- |
| `FailureThreshold` | `int` | 闭合状态达到多少次失败后打开 | `5` |
| `Timeout` | `time.Duration` | 打开后等待多久允许恢复探测 | `5 * time.Second` |
| `SuccessThreshold` | `int` | 半开成功探测达到多少次后闭合 | `1` |
| `HalfOpenMaxConcurrent` | `int` | 半开时同时执行的探测上限 | `1` |
| `Interval` | `time.Duration` | 闭合状态失败计数的重置窗口；0 表示累积到打开 | `0` |
| `IsFailure` | `func(*gin.Context, error) bool` | 判断本次请求是否计为失败 | 新增 Gin 错误或状态码 ≥ 500 |
| `Clock` | `func() time.Time` | 状态机读取的时钟，便于确定性测试 | `time.Now` |
| `OnOpen` | `gin.HandlerFunc` | 响应因 Open 被拒绝的请求 | 503 JSON |
| `OnHalfOpen` | `gin.HandlerFunc` | 响应因半开名额已满被拒绝的请求 | 429 JSON |
| `OnClose` | `gin.HandlerFunc` | 成功探测关闭熔断器后的通知 | 无操作 |

`New` 对非正的失败阈值、成功阈值、恢复等待时间及探测并发上限应用默认值，与上游一致。非正的 Interval 不启用窗口重置；生产环境建议显式设置正值。

配置在 New 时复制。Clock 和回调必须并发安全；不要并发修改全局 `DefaultConfig`，不要复制已经使用的 CircuitBreaker。

### 回调语义

- `OnOpen` 每次有请求因 Open 被拒绝时执行；达到失败阈值、刚打开的那一刻不会调用它。
- `OnHalfOpen` 每次有探测因半开名额已满被拒绝时执行；进入 Half-Open 本身不会调用它。
- `OnClose` 在成功探测关闭熔断器后调用一次，此时业务处理器已经完成。它仅用于记录恢复，不应写响应或再次调用 `Next`；手动 ForceClose/Reset 不会调用它。

因此 OnOpen/OnHalfOpen 可用于记录拒绝请求数，不能直接当作状态转换事件。回调在请求 goroutine 上执行，不应保留 `*gin.Context`。

### Gin 的错误与 Recovery

Gin 的 `Next()` 不返回 error。本包将中间件之后处理链新增的 `c.Errors` 用 `errors.Join` 合并后交给 IsFailure，原始错误可以通过 `errors.Is/As` 检查。HTTP 400 本身不计为默认失败；如果同时通过 `c.Error(err)` 记录错误，默认策略仍会计为失败，可通过 IsFailure 排除预期业务错误。

传播到熔断器的 panic 会计为失败并重新抛出，让外层 Recovery 响应。Recovery 在内层时，默认 IsFailure 会将它生成的 500 响应计为失败。自定义 IsFailure 需自行考虑内层 Recovery 的响应。

## 运维控制

| 方法 | 作用 |
| --- | --- |
| `GetState()` / `IsOpen()` | 查询当前状态，先应用已到期的 Open → Half-Open 转换 |
| `Metrics()` | 基础状态、失败/成功计数、总请求数和拒绝数 |
| `GetStateStats()` | 基础计数、阈值、状态转换时间、恢复等待时间和窗口到期时间 |
| `HealthHandler()` | Open 返回 503，其它状态返回 200，包含 state 和 healthy |
| `ForceOpen()` | 强制打开并持续保持打开 |
| `ForceClose()` / `Reset()` | 闭合、清零计数并开始新的失败计数窗口 |
| `SetTimeout(d)` | 调整恢复等待时间，也作用于已经打开的实例 |

ForceOpen 不随 Timeout 自动恢复，需要 ForceClose 或 Reset 结束。SetTimeout 不校验参数，生产配置应传正值。

Metrics 是基础观测快照；GetStateStats 在同一锁内读取状态相关字段，避免状态与时间戳来自不同转换。请求计数通过原子操作更新，观测结果不应作为额外的请求准入依据。

`totalRequests` 和 `rejectedRequests` 是实例生命周期的累积计数，前者包含被拒绝的请求。`failures` 和 `successes` 是状态机计数，会随窗口或状态转换重置，不能当作生命周期累计失败与成功数。

HealthHandler 报告熔断状态，不主动调用下游。在半开时返回 200，但 JSON 的 `healthy` 只有闭合时才为 true，表示探测已经可用，完整恢复仍待确认。健康检查和指标端点应注册在受保护路由组外；状态查询或指标抓取也可能触发到期恢复。

熔断器不启动后台任务，无需退出时停止。`Stop()` 保留上游废弃接口，是无操作。

## Gin 使用示例

后续片段假定应用已有 `router := gin.New()` 并安装 `gin.Recovery()`，按示例使用的标识符补充相应 import。示例均以当前包接口编译验证。

### 1. 基础设置：保护所有业务路由

下面是可运行的程序。通过根路由组给所有业务接口挂载同一个实例，健康和状态接口独立注册，熔断打开后仍能查看状态。

```go
package main

import (
    "log"
    "net/http"
    "time"

    "github.com/gin-gonic/gin"
    "github.com/apus-run/gala/components/ginx/middlewares/circuitbreaker"
)

func main() {
    router := gin.New()
    router.Use(gin.Recovery())

    cb := circuitbreaker.New(circuitbreaker.Config{
        FailureThreshold: 3,
        Timeout:          5 * time.Second,
        SuccessThreshold: 2,
        Interval:         30 * time.Second,
    })

    router.GET("/health/circuit", cb.HealthHandler())
    router.GET("/metrics/circuit", func(c *gin.Context) {
        c.JSON(http.StatusOK, cb.GetStateStats())
    })

    business := router.Group("", cb.Build())
    business.GET("/", func(c *gin.Context) {
        c.String(http.StatusOK, "Hello, world!")
    })

    if err := router.Run("127.0.0.1:3000"); err != nil {
        log.Fatal(err)
    }
}
```

根路由组适用于所有业务接口共享同一故障边界的应用。多个独立服务通常应使用第 8 节的不同实例。

### 2. 指定路由与路由组

```go
cb := circuitbreaker.New(circuitbreaker.Config{
    FailureThreshold: 3,
    Interval:         30 * time.Second,
})

router.GET("/protected", circuitbreaker.Middleware(cb), func(c *gin.Context) {
    c.String(http.StatusOK, "Protected service running")
})

api := router.Group("/api", cb.Build())
api.GET("/users", func(c *gin.Context) {
    c.JSON(http.StatusOK, gin.H{"users": []string{"alice"}})
})
api.POST("/users", func(c *gin.Context) {
    c.Status(http.StatusCreated)
})
```

同一 cb 在这些路由间共享失败计数和状态。只有属于同一故障边界的接口才应共用。

### 3. 自定义拒绝响应和恢复通知

```go
cb := circuitbreaker.New(circuitbreaker.Config{
    FailureThreshold: 3,
    Timeout:          10 * time.Second,
    Interval:         30 * time.Second,
    OnOpen: func(c *gin.Context) {
        c.Header("Retry-After", "10") // 示例重试建议，非实时剩余秒数。
        c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
            "error": "依赖暂不可用，请稍后重试",
        })
    },
    OnHalfOpen: func(c *gin.Context) {
        c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
            "error": "恢复探测名额已满",
        })
    },
    OnClose: func(c *gin.Context) {
        log.Printf("circuit recovered: %s", c.FullPath())
    },
})

router.GET("/custom", cb.Build(), func(c *gin.Context) {
    c.String(http.StatusOK, "Protected service running")
})
```

中间件在拒绝时会先 Abort；自定义回调负责生成响应。OnClose 完成通知即可，避免修改业务处理器已生成的响应。

### 4. 保护外部 API 调用

给 HTTP 客户端设置自己的执行超时，并将 Gin 请求 context 传给下游。此例展示状态查询型接口：调用成功后返回上游状态，不代理其响应正文。

```go
client := &http.Client{Timeout: 2 * time.Second}
cb := circuitbreaker.New(circuitbreaker.Config{
    FailureThreshold: 3,
    Timeout:          5 * time.Second,
    Interval:         30 * time.Second,
})

router.GET("/external-api", cb.Build(), func(c *gin.Context) {
    req, err := http.NewRequestWithContext(
        c.Request.Context(), http.MethodGet, "https://example.com/api", nil,
    )
    if err != nil {
        _ = c.Error(err)
        c.AbortWithStatus(http.StatusInternalServerError)
        return
    }
    resp, err := client.Do(req)
    if err != nil {
        _ = c.Error(err)
        c.AbortWithStatus(http.StatusBadGateway)
        return
    }
    defer resp.Body.Close()

    if resp.StatusCode >= http.StatusInternalServerError {
        c.AbortWithStatus(http.StatusBadGateway)
        return
    }
    c.JSON(http.StatusOK, gin.H{"upstream_status": resp.StatusCode})
})
```

连接错误、客户端超时及上游 5xx 在这里都会被默认 IsFailure 计为失败。熔断器只在该路由执行过程中观测结果；如果一个请求会调用多个独立依赖，需要进一步拆分保护范围。

### 5. 半开阶段的并发探测

```go
cb := circuitbreaker.New(circuitbreaker.Config{
    FailureThreshold:      3,
    Timeout:               5 * time.Second,
    SuccessThreshold:      2,
    HalfOpenMaxConcurrent: 2,
    Interval:              30 * time.Second,
})

router.GET("/half-open-limit", cb.Build(), func(c *gin.Context) {
    timer := time.NewTimer(2 * time.Second) // 模拟耗时操作。
    defer timer.Stop()
    select {
    case <-timer.C:
        c.String(http.StatusOK, "Probe completed")
    case <-c.Request.Context().Done():
        _ = c.Error(c.Request.Context().Err())
        c.AbortWithStatus(http.StatusGatewayTimeout)
    }
})
```

进入半开后最多有 2 个探测同时执行，第 3 个被拒绝。闭合状态不会套用这个并发上限，日常资源保护可组合 activelimit。

### 6. Prometheus 指标与日志

这里分别记录拒绝请求数、成功探测恢复次数，并通过 Gauge 暴露当前状态。使用私有 Registry，避免重复注册同名指标。

```go
rejected := prometheus.NewCounterVec(prometheus.CounterOpts{
    Name: "circuit_rejected_requests_total",
    Help: "Requests refused by the circuit breaker.",
}, []string{"state"})
recovered := prometheus.NewCounter(prometheus.CounterOpts{
    Name: "circuit_recoveries_total",
    Help: "Half-open probes that closed the circuit.",
})

cb := circuitbreaker.New(circuitbreaker.Config{
    FailureThreshold: 5,
    Timeout:          10 * time.Second,
    Interval:         30 * time.Second,
    OnOpen: func(c *gin.Context) {
        rejected.WithLabelValues("open").Inc()
        c.AbortWithStatus(http.StatusServiceUnavailable)
    },
    OnHalfOpen: func(c *gin.Context) {
        rejected.WithLabelValues("half-open").Inc()
        c.AbortWithStatus(http.StatusTooManyRequests)
    },
    OnClose: func(c *gin.Context) {
        recovered.Inc()
        log.Printf("circuit recovered: %s", c.FullPath())
    },
})
state := prometheus.NewGaugeFunc(prometheus.GaugeOpts{
    Name: "circuit_state",
    Help: "Circuit state: closed=0, half-open=1, open=2.",
}, func() float64 {
    switch cb.GetState() {
    case circuitbreaker.StateHalfOpen:
        return 1
    case circuitbreaker.StateOpen:
        return 2
    default:
        return 0
    }
})

registry := prometheus.NewRegistry()
registry.MustRegister(rejected, recovered, state)
router.GET("/metrics", gin.WrapH(promhttp.HandlerFor(registry, promhttp.HandlerOpts{})))
router.GET("/observed", cb.Build(), func(c *gin.Context) { c.Status(http.StatusOK) })
```

补充导入 `github.com/prometheus/client_golang/prometheus` 和 `github.com/prometheus/client_golang/prometheus/promhttp`。OnOpen 按被拒绝的请求计数，指标名称因此使用 rejected_requests，不能据此统计打开状态转换次数。多个实例注册到同一个 Registry 时应使用不同标签或相应的向量指标。

### 7. 失败计数的重置窗口

```go
cb := circuitbreaker.New(circuitbreaker.Config{
    FailureThreshold: 5,
    Timeout:          10 * time.Second,
    Interval:         30 * time.Second,
})
router.GET("/windowed", cb.Build(), func(c *gin.Context) {
    c.Status(http.StatusInternalServerError)
})
```

计数窗口从构造、闭合或上一次到期重置时开始。假设窗口内已发生 4 次失败，下一次失败在窗口到期后报告，计数会先清零再变成 1，而不是累加到 5。重置由失败报告触发，是固定窗口行为，不是滑动窗口，也不是失败率统计；成功请求不会重置闭合状态失败计数。Interval 为 0 时，零散失败会一直累积到打开。

### 8. 不同服务使用不同熔断器

```go
dbCB := circuitbreaker.New(circuitbreaker.Config{
    FailureThreshold: 5,
    Timeout:          10 * time.Second,
    Interval:         30 * time.Second,
})
apiCB := circuitbreaker.New(circuitbreaker.Config{
    FailureThreshold: 3,
    Timeout:          5 * time.Second,
    Interval:         30 * time.Second,
})

router.GET("/db-service", dbCB.Build(), func(c *gin.Context) {
    c.String(http.StatusOK, "Database-backed service")
})
router.GET("/api-service", apiCB.Build(), func(c *gin.Context) {
    c.String(http.StatusOK, "External API service")
})
router.GET("/health/db", dbCB.HealthHandler())
router.GET("/health/api", apiCB.HealthHandler())
```

这里的处理器仅展示挂载位置，应用需替换成实际调用，并使用请求 context 和下游超时。两个熔断器的阈值、计数和状态独立，某个服务打开不会改变另一个服务的状态。

## 与上游的能力对齐

对齐基准是用于本次移植的 `contrib-main/v3/circuitbreaker` 源码与 README。

| 能力 | 对齐情况 |
| --- | --- |
| Closed / Open / Half-Open 与阈值 | 保留，同样的默认值与状态转换逻辑 |
| 半开并发限制、成功关闭、失败重开 | 保留 |
| 状态窗口代际隔离 | 保留，旧请求结果与释放不会影响新窗口 |
| Interval 与可注入 Clock | 保留，同样的固定窗口及惰性恢复语义 |
| 自定义故障判定与三个回调 | 保留，输入和回调签名适配 Gin |
| Metrics / GetStateStats | 保留字段与数值语义，返回类型改为 gin.H |
| HealthHandler | 保留 HTTP 状态及 JSON 字段，返回 Gin handler |
| ForceOpen / ForceClose / Reset / SetTimeout | 保留 |
| 废弃的手动协议与 Stop | 保留 |
| Build | Gin 版新增便捷方法，等价于 Middleware |
| panic 与拒绝 Abort | Gin 版增强，支持 Recovery 配合及防止拒绝后继续业务链 |

上游所有公开函数与方法均保留，30 项上游测试均已适配。语法树对比确认：将返回 map 的 Fiber 类型规范化为 gin.H 后，29 个函数体保持一致；另 3 个函数体是 Middleware、serve 和 HealthHandler，属于框架适配边界。另有 4 组 Gin 适配测试覆盖新增错误、Recovery 顺序及拒绝请求。

Gin 版与 Fiber 版的调用代码不能直接互换：Fiber 的处理器和响应回调返回 error，Gin 使用 `gin.HandlerFunc`，错误通过 `c.Error` 记录。两者都不内置下游执行超时、慢请求成功率判定、分布式全局状态或通用状态转换事件回调。

## 验证与使用边界

在 `components/ginx` 模块目录运行：

```sh
go test -race ./middlewares/circuitbreaker
go vet ./middlewares/circuitbreaker
go test -run '^$' -bench BenchmarkCircuitBreaker -benchmem ./middlewares/circuitbreaker
```

测试覆盖状态转换、恢复时刻、半开并发、跨窗口释放、旧请求结果、强制打开、统计一致性，以及 Gin 的错误传递、拒绝 Abort 和 panic/Recovery 配合。

熔断器状态位于当前进程，各副本独立判断。默认 IsFailure 不将耗时很长但成功返回 200 的请求计为失败。Timeout 控制打开后的恢复等待时间，下游 HTTP、数据库和 RPC 仍需配置自己的执行超时；闭合状态并发保护可与 [activelimit](../activelimit/README.md) 配合。
