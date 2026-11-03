// Package ginbreaker demonstrates a Gin middleware built on the
// transport-agnostic circuitbreaker in ginx's NewBuilder chaining style
// | 演示在通用 circuitbreaker 之上按 ginx 的 NewBuilder 链式风格构建 Gin 中间件。
//
// # Notes | 说明
//
// Chain setters on a [Builder] then Build; NewInternalServiceBuilder and
// NewExternalAPIBuilder provide the internal-service and third-party-API
// profiles. Divergences from ginx are deliberate: gobreaker v2 has no
// injectable clock, and its half-open close threshold is MaxRequests itself,
// so SuccessThreshold cannot be expressed.
// 在 [Builder] 上链式调用 setter 后 Build；NewInternalServiceBuilder 与
// NewExternalAPIBuilder 分别是内部服务与第三方 API 的预设。与 ginx 的差异是有意
// 的：gobreaker v2 无可注入时钟，半开闭合阈值就是 MaxRequests，无法表达
// SuccessThreshold。
//
// # Example | 示例
//
//	b := ginbreaker.NewInternalServiceBuilder()
//	r := gin.Default()
//	r.Use(b.Build())
//	r.GET("/health", ginbreaker.HealthHandler(b.Breaker()))
package ginbreaker

import (
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	gobreaker "github.com/sony/gobreaker/v2"

	"github.com/apus-run/gala/components/circuitbreaker"
)

// Builder assembles the middleware. Chain setters, then Build; Breaker exposes
// the built breaker for ForceOpen/ForceClose/Reset and Stats. Start from
// NewInternalServiceBuilder or NewExternalAPIBuilder for the keystoneGo
// profiles, or NewBuilder for the ginx middleware's defaults.
// Builder 组装中间件。链式调用 setter 后 Build；Breaker 暴露构建好的熔断器，
// 供 ForceOpen/ForceClose/Reset 与 Stats 使用。keystoneGo 的预设从
// NewInternalServiceBuilder 或 NewExternalAPIBuilder 出发，ginx 中间件的默认值
// 用 NewBuilder。
type Builder struct {
	failureThreshold int
	timeout          time.Duration
	maxRequests      uint32
	interval         time.Duration
	// rateMinRequests/failureRate switch tripping from consecutive failures to
	// a failure rate once SetFailureRate configured one.
	// rateMinRequests/failureRate 用于在 SetFailureRate 配置过失败率后，
	// 把熔断条件从连续失败切换为失败率。
	rateMinRequests uint32
	failureRate     float64
	isFailure       func(c *gin.Context, err error) bool
	onOpen          gin.HandlerFunc
	onHalfOpen      gin.HandlerFunc
	onClose         func()
	onStateChange   func(from, to gobreaker.State)
	breaker         *circuitbreaker.Breaker
}

// NewBuilder returns the ginx middleware's defaults: trip after 5 consecutive
// failures, 5s open timeout, 1 half-open probe; err or status >= 500 counts as
// failure; open refuses with 503, half-open saturation with 429
// | NewBuilder 返回 ginx 中间件的默认值：连续失败 5 次熔断、熔断 5 秒、
// 半开放行 1 个探测；err 或状态码 >= 500 记为失败；熔断返回 503，
// 半开放行饱和返回 429。
func NewBuilder() *Builder {
	return &Builder{
		failureThreshold: 5,
		timeout:          5 * time.Second,
		maxRequests:      1,
		isFailure: func(c *gin.Context, err error) bool {
			return err != nil || c.Writer.Status() >= http.StatusInternalServerError
		},
		onOpen: func(c *gin.Context) {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "service unavailable"})
		},
		onHalfOpen: func(c *gin.Context) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "service under recovery"})
		},
	}
}

// SetFailureThreshold sets the consecutive failures that trip the circuit
// | SetFailureThreshold 设置触发熔断的连续失败次数.
func (b *Builder) SetFailureThreshold(n int) *Builder { b.failureThreshold = n; return b }

// SetTimeout sets the open duration before half-open probes are admitted
// | SetTimeout 设置熔断打开时长，到期后半开探测才被放行.
func (b *Builder) SetTimeout(d time.Duration) *Builder { b.timeout = d; return b }

// SetMaxRequests sets the half-open probes admitted concurrently. Upstream
// coupling: the circuit also closes only after this many consecutive successes.
// SetMaxRequests 设置半开时并发放行的探测数。上游耦合：也只有这么多探测连续成功，
// 熔断器才会闭合。
func (b *Builder) SetMaxRequests(n uint32) *Builder { b.maxRequests = n; return b }

// SetInterval sets the closed-state count window. Zero means failures
// accumulate until tripping.
// SetInterval 设置闭合状态的计数窗口。0 表示失败持续累积直到熔断。
func (b *Builder) SetInterval(d time.Duration) *Builder { b.interval = d; return b }

// SetIsFailure replaces the failure detector; default counts err or >= 500
// | SetIsFailure 替换失败判定；默认 err 或状态码 >= 500 记为失败.
func (b *Builder) SetIsFailure(fn func(*gin.Context, error) bool) *Builder {
	b.isFailure = fn
	return b
}

// SetOnOpen replaces the open refusal response (default 503)
// | SetOnOpen 替换熔断打开时的拒绝响应（默认 503）.
func (b *Builder) SetOnOpen(h gin.HandlerFunc) *Builder { b.onOpen = h; return b }

// SetOnHalfOpen replaces the half-open saturation response (default 429)
// | SetOnHalfOpen 替换半开放行饱和时的响应（默认 429）.
func (b *Builder) SetOnHalfOpen(h gin.HandlerFunc) *Builder { b.onHalfOpen = h; return b }

// SetOnClose notifies after a transition to closed, once the upstream lock
// has been released. The callback may query the breaker state and metrics.
// SetOnClose 在切换到闭合状态后通知，此时上游锁已释放；
// 回调里可以查询熔断器状态与指标。
func (b *Builder) SetOnClose(fn func()) *Builder { b.onClose = fn; return b }

// SetFailureRate switches tripping from consecutive failures to a failure
// rate: the circuit opens once at least minRequests requests in the current
// window failed at rate or more of the time (0.5 = 50%). ReadyToTrip is
// consulted on failures only, so the trip lands on a failing request.
// SetFailureRate 把熔断条件从连续失败切换为失败率：当前窗口内至少 minRequests
// 个请求、失败占比达到 rate 及以上（0.5 = 50%）即熔断。ReadyToTrip 只在失败时
// 被咨询，因此熔断总是落在某个失败的请求上。
func (b *Builder) SetFailureRate(minRequests int, rate float64) *Builder {
	b.rateMinRequests, b.failureRate = uint32(minRequests), rate
	return b
}

// SetOnStateChange observes every transition, delivered after the upstream
// lock is released like OnClose. Use it for transition logging.
// SetOnStateChange 观察每次状态切换，与 OnClose 一样在上游锁释放后投递。
// 适合用于状态切换日志。
func (b *Builder) SetOnStateChange(fn func(from, to gobreaker.State)) *Builder {
	b.onStateChange = fn
	return b
}

// NewInternalServiceBuilder is the internal service-to-service profile
// (keystoneGo's DefaultBreaker): tolerate noise, trip only after 6
// consecutive failures in a 60s window, then probe 3 at a time after 30s.
// NewInternalServiceBuilder 是内部服务间调用的预设（keystoneGo 的
// DefaultBreaker）：容忍噪声，60 秒窗口内连续失败 6 次才熔断，
// 30 秒后每次放行 3 个探测。
func NewInternalServiceBuilder() *Builder {
	return NewBuilder().
		SetFailureThreshold(6).
		SetMaxRequests(3).
		SetTimeout(30 * time.Second).
		SetInterval(60 * time.Second)
}

// NewExternalAPIBuilder is the third-party API profile (keystoneGo's
// ExternalAPIBreaker): fail fast — trip once ≥50% of ≥5 requests in a 60s
// window failed, then probe 5 at a time after a 60s cool-down.
// NewExternalAPIBuilder 是第三方 API 的预设（keystoneGo 的
// ExternalAPIBreaker）：快速失败——60 秒窗口内 ≥5 个请求且失败率 ≥50% 即熔断，
// 60 秒冷却后每次放行 5 个探测。
func NewExternalAPIBuilder() *Builder {
	return NewBuilder().
		SetFailureRate(5, 0.5).
		SetMaxRequests(5).
		SetTimeout(60 * time.Second).
		SetInterval(60 * time.Second)
}

// Breaker returns the breaker built by Build; nil before that
// | Breaker 返回 Build 构建的熔断器；Build 之前为 nil.
func (b *Builder) Breaker() *circuitbreaker.Breaker { return b.breaker }

// Build assembles the breaker and its middleware | Build 组装熔断器与中间件.
func (b *Builder) Build() gin.HandlerFunc {
	threshold, minRequests, rate := uint32(b.failureThreshold), b.rateMinRequests, b.failureRate
	readyToTrip := func(c gobreaker.Counts) bool { return c.ConsecutiveFailures >= threshold }
	if rate > 0 {
		readyToTrip = func(c gobreaker.Counts) bool {
			return c.Requests >= minRequests && float64(c.TotalFailures)/float64(c.Requests) >= rate
		}
	}
	onClose, onStateChange := b.onClose, b.onStateChange
	// OnStateChange runs under gobreaker's lock. Queue every transition and
	// deliver after Allow/done returns so callbacks can safely inspect the
	// breaker; the flushing request is one of the requests that caused them.
	// OnStateChange 在 gobreaker 的锁内执行。把每次切换排队，等 Allow/done
	// 返回后再投递，回调就能安全地查看熔断器；执行冲刷的请求正是引发这些切换的
	// 请求之一。
	var mu sync.Mutex
	var queued []func()
	b.breaker = circuitbreaker.New(gobreaker.Settings{
		Name:        "ginbreaker",
		MaxRequests: b.maxRequests,
		Interval:    b.interval,
		Timeout:     b.timeout,
		ReadyToTrip: readyToTrip,
		OnStateChange: func(_ string, from, to gobreaker.State) {
			mu.Lock()
			defer mu.Unlock()
			queued = append(queued, func() {
				if onStateChange != nil {
					onStateChange(from, to)
				}
				if to == gobreaker.StateClosed && onClose != nil {
					onClose()
				}
			})
		},
	})
	return b.middleware(func() {
		mu.Lock()
		drain := queued
		queued = nil
		mu.Unlock()
		for _, notify := range drain {
			notify()
		}
	})
}

// middleware adapts Gin requests to the breaker. Panics count as failures and
// are rethrown to the application's Recovery middleware, whichever side it is on.
// Built handlers capture a copy: later builder changes do not affect them,
// matching the ginx middleware's builder contract.
// middleware 把 Gin 请求适配到熔断器。panic 记为失败并重新抛给应用的
// Recovery 中间件（无论它在哪一侧）。构建出的 handler 捕获一份副本：
// 之后修改 builder 不影响它们，与 ginx 中间件的 builder 契约一致。
func (b *Builder) middleware(notifyClosed func()) gin.HandlerFunc {
	breaker, onOpen, onHalfOpen, isFailure := b.breaker, b.onOpen, b.onHalfOpen, b.isFailure
	return func(c *gin.Context) {
		defer notifyClosed()
		done, err := breaker.Allow()
		if err != nil {
			c.Abort()
			if errors.Is(err, circuitbreaker.ErrTooManyRequests) {
				onHalfOpen(c)
			} else {
				onOpen(c)
			}
			return
		}
		defer func() {
			if value := recover(); value != nil {
				done(fmt.Errorf("panic: %v", value))
				panic(value)
			}
		}()
		previous := len(c.Errors)
		c.Next()
		var failures []error
		if len(c.Errors) > previous {
			for _, failure := range c.Errors[previous:] {
				failures = append(failures, failure.Err)
			}
		}
		if isFailure(c, errors.Join(failures...)) {
			done(errors.New("failure"))
			return
		}
		done(nil)
	}
}

// HealthHandler reports 503 while open, 200 otherwise. Register it outside the
// protected route group so an open circuit cannot hide it.
// HealthHandler 在熔断打开时返回 503，其余时间 200。把它注册在受保护路由组之外，
// 熔断打开时它才不会被一并挡住。
func HealthHandler(b *circuitbreaker.Breaker) gin.HandlerFunc {
	return func(c *gin.Context) {
		s := b.Stats()
		status := http.StatusOK
		if s.State == gobreaker.StateOpen {
			status = http.StatusServiceUnavailable
		}
		c.JSON(status, gin.H{"state": s.State.String(), "healthy": s.State != gobreaker.StateOpen, "total": s.Total, "rejected": s.Rejected})
	}
}
