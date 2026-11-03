// Package circuitbreaker wraps gobreaker's two-step breaker for
// transport-agnostic admission control | 封装 gobreaker 的两段式熔断器，做与传输协议无关的准入控制。
//
// # Notes | 说明
//
// [Breaker] adds the operations surface the ginx middleware keeps on top of
// gobreaker's TwoStepCircuitBreaker: ForceOpen/ForceClose/Reset controls and
// cumulative stats via [Breaker.Stats]. Write your middleware (or any
// caller) against [Breaker.Allow] and the done it returns; for outbound
// calls, gobreaker's CircuitBreaker[T].Execute may fit better. Kept-honest
// upstream limits: there is no injectable clock, and the half-open close
// threshold is MaxRequests itself — every probe must succeed to close.
// [Breaker] 在 gobreaker 的 TwoStepCircuitBreaker 之上补齐 ginx 中间件需要的
// 运维面：ForceOpen/ForceClose/Reset 强制控制，以及经 [Breaker.Stats] 输出的
// 累计指标。基于 [Breaker.Allow] 与其返回的 done 编写中间件（或任意调用方）；
// 出站调用直接用 gobreaker 的 CircuitBreaker[T].Execute 可能更合适。上游限制
// 如实保留：无可注入时钟；半开闭合阈值就是 MaxRequests——所有探测都成功才闭合。
//
// # Features | 功能
//
// - Two-step Allow/done admission | 两段式 Allow/done 准入
// - ForceOpen, ForceClose and Reset overrides | ForceOpen、ForceClose、Reset 强制开关
// - Stats snapshot with effective state and cumulative counters | 含生效状态与累计计数的 Stats 快照
//
// # Example | 示例
//
//	b := New(Settings{Name: "svc"})
//	done, err := b.Allow()
//	if err == nil {
//		done(call())
//	}
package circuitbreaker

import (
	"sync/atomic"

	gobreaker "github.com/sony/gobreaker/v2"
)

// Settings is gobreaker's own; every field and hook applies unchanged
// | Settings 就是 gobreaker 自带的配置，所有字段与钩子原样生效.
type Settings = gobreaker.Settings

// ErrOpenState and ErrTooManyRequests are the refusal reasons. Map
// ErrOpenState to 503 and ErrTooManyRequests to 429 in an HTTP middleware;
// both are also returned while ForceOpen is active.
// ErrOpenState 与 ErrTooManyRequests 是拒绝原因。HTTP 中间件里把 ErrOpenState
// 映射为 503、ErrTooManyRequests 映射为 429；ForceOpen 生效期间两者同样会返回。
var (
	ErrOpenState       = gobreaker.ErrOpenState
	ErrTooManyRequests = gobreaker.ErrTooManyRequests
)

// Admission result of a forced breaker, reported before the machine's own
// | 强制模式的准入结果，优先于熔断器自身状态机上报.
const (
	forceAuto int32 = iota
	forceOpen
	forceClose
)

// Breaker is a two-step circuit breaker: Allow decides, the returned done
// reports the outcome. Construct with New; do not copy. A done call belongs to
// the Allow that returned it; on a panic in the protected work the caller must
// still reach done with a non-nil error (defer is the usual shape).
// Breaker 是两段式熔断器：Allow 做准入判定，返回的 done 上报结果。
// 用 New 构造，不要复制。done 只属于返回它的那次 Allow；被保护代码 panic 时，
// 调用方仍须以非 nil 错误触达 done（惯用写法是 defer）。
type Breaker struct {
	cb       *gobreaker.TwoStepCircuitBreaker[struct{}]
	forced   atomic.Int32
	total    atomic.Uint64
	rejected atomic.Uint64
}

// New assembles a breaker from gobreaker settings | New 用 gobreaker 配置组装熔断器.
func New(s Settings) *Breaker {
	return &Breaker{cb: gobreaker.NewTwoStepCircuitBreaker[struct{}](s)}
}

// Allow admits one request and returns its outcome reporter. A refused call
// returns ErrOpenState or ErrTooManyRequests and a nil done.
// Allow 准入一个请求并返回其结果上报函数。被拒绝的调用返回
// ErrOpenState 或 ErrTooManyRequests，done 为 nil。
func (b *Breaker) Allow() (func(error), error) {
	b.total.Add(1)
	switch b.forced.Load() {
	case forceOpen:
		b.rejected.Add(1)
		return nil, ErrOpenState
	case forceClose:
		// bypassed: outcomes are not recorded
		// 被旁路：结果不记录
		return func(error) {}, nil
	}
	done, err := b.cb.Allow()
	if err != nil {
		b.rejected.Add(1)
		return nil, err
	}
	return done, nil
}

// State reports the effective state, forced modes included
// | State 上报生效状态（含强制模式）.
func (b *Breaker) State() gobreaker.State {
	switch b.forced.Load() {
	case forceOpen:
		return gobreaker.StateOpen
	case forceClose:
		return gobreaker.StateClosed
	}
	return b.cb.State()
}

// ForceOpen refuses every Allow with ErrOpenState until Reset
// | ForceOpen 拒绝所有 Allow 并返回 ErrOpenState，直到 Reset.
func (b *Breaker) ForceOpen() { b.forced.Store(forceOpen) }

// ForceClose admits everything without recording outcomes until Reset
// | ForceClose 放行一切且不记录结果，直到 Reset.
func (b *Breaker) ForceClose() { b.forced.Store(forceClose) }

// Reset returns control to the breaker's own state machine.
// Reset 恢复由熔断器自身状态机接管。
// shortcut: gobreaker v2 has no count reset, so the underlying window keeps its history.
// shortcut: gobreaker v2 不支持计数清零，底层窗口会保留历史。
func (b *Breaker) Reset() { b.forced.Store(forceAuto) }

// Stats is a snapshot: the effective state, cumulative admission counts and
// gobreaker's current window counts.
// Stats 是一份快照：生效状态、累计准入计数与 gobreaker 当前窗口计数。
type Stats struct {
	State gobreaker.State
	// Total is Allow calls since construction, forced modes included | 自构造以来的 Allow 调用数（含强制模式）.
	Total uint64
	// Rejected is refusals, forced ones included | 拒绝次数（含强制拒绝）.
	Rejected uint64
	// Counts is gobreaker's current window counts | gobreaker 当前窗口计数.
	Counts gobreaker.Counts
}

// Stats snapshots the breaker. Counters are atomic, not transactional.
// Stats 给熔断器做快照。计数是原子操作，非事务一致。
func (b *Breaker) Stats() Stats {
	return Stats{
		State:    b.State(),
		Total:    b.total.Load(),
		Rejected: b.rejected.Load(),
		Counts:   b.cb.Counts(),
	}
}
