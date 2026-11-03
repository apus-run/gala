package circuitbreaker

import (
	"time"

	"github.com/gin-gonic/gin"
)

// Builder assembles a CircuitBreaker middleware with ginx's chaining style.
// New applies DefaultConfig to every unset field, so the zero Builder is a
// valid starting point; Build constructs the circuit and Breaker exposes it
// for ForceOpen/ForceClose and metrics. Do not reuse a Builder after Build.
// Builder 以 ginx 链式风格组装 CircuitBreaker 中间件。未设置的字段沿用
// DefaultConfig，零值 Builder 即可用；Build 构建熔断器，Breaker 暴露它供
// ForceOpen/ForceClose 与指标使用。Build 之后不要复用 Builder。
type Builder struct {
	config  Config
	breaker *CircuitBreaker
}

// NewBuilder creates an empty Builder | NewBuilder 创建空 Builder。
func NewBuilder() *Builder {
	return &Builder{}
}

// SetFailureThreshold sets the failure count that trips the circuit.
// SetFailureThreshold 设置触发熔断的失败次数。
func (b *Builder) SetFailureThreshold(n int) *Builder { b.config.FailureThreshold = n; return b }

// SetTimeout sets how long the circuit stays open before recovery probes;
// it is not an execution timeout.
// SetTimeout 设置熔断打开到允许恢复探测的时长；它不是执行超时。
func (b *Builder) SetTimeout(d time.Duration) *Builder { b.config.Timeout = d; return b }

// SetSuccessThreshold sets the successful half-open probes that close the circuit.
// SetSuccessThreshold 设置关闭熔断所需的半开成功探测次数。
func (b *Builder) SetSuccessThreshold(n int) *Builder { b.config.SuccessThreshold = n; return b }

// SetHalfOpenMaxConcurrent sets the maximum concurrent probes in half-open state.
// SetHalfOpenMaxConcurrent 设置半开状态的最大并发探测数。
func (b *Builder) SetHalfOpenMaxConcurrent(n int) *Builder {
	b.config.HalfOpenMaxConcurrent = n
	return b
}

// SetInterval sets the failure-count window; zero means failures accumulate
// until the circuit opens.
// SetInterval 设置失败计数窗口；零表示失败一直累计直到熔断打开。
func (b *Builder) SetInterval(d time.Duration) *Builder { b.config.Interval = d; return b }

// SetIsFailure replaces the failure detector (default: err or status >= 500).
// SetIsFailure 替换失败判定函数（默认：err 或状态码 >= 500）。
func (b *Builder) SetIsFailure(fn func(*gin.Context, error) bool) *Builder {
	b.config.IsFailure = fn
	return b
}

// SetClock replaces the time source, which drives every state transition; for
// tests only.
// SetClock 替换驱动所有状态迁移的时间源；仅供测试。
func (b *Builder) SetClock(fn func() time.Time) *Builder { b.config.Clock = fn; return b }

// SetOnOpen replaces the response for requests refused by an open circuit.
// SetOnOpen 替换熔断打开时拒绝请求的响应。
func (b *Builder) SetOnOpen(h gin.HandlerFunc) *Builder { b.config.OnOpen = h; return b }

// SetOnHalfOpen replaces the response for requests refused by half-open saturation.
// SetOnHalfOpen 替换半开探测额满时拒绝请求的响应。
func (b *Builder) SetOnHalfOpen(h gin.HandlerFunc) *Builder { b.config.OnHalfOpen = h; return b }

// SetOnClose registers the notification that runs after the probe closing the
// circuit. It is a notification, not a response writer.
// SetOnClose 注册关闭熔断的探测成功后运行的回调。它是通知，不是响应写入器。
func (b *Builder) SetOnClose(h gin.HandlerFunc) *Builder { b.config.OnClose = h; return b }

// Breaker returns the circuit built by Build; nil before that.
// Breaker 返回 Build 构建的熔断器；在此之前为 nil。
func (b *Builder) Breaker() *CircuitBreaker { return b.breaker }

// Build constructs the circuit from the chained settings and returns its middleware.
// Build 按链式配置构建熔断器并返回其中间件。
func (b *Builder) Build() gin.HandlerFunc {
	b.breaker = New(b.config)
	return Middleware(b.breaker)
}
