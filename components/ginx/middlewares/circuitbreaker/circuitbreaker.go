// Copyright (c) 2021 Fiber. Adapted from gofiber/contrib/v3/circuitbreaker.
// Licensed under the MIT License; see LICENSE.

// Package circuitbreaker provides a Gin circuit breaker with bounded recovery probes.
// circuitbreaker 提供带受限恢复探测的 Gin 熔断器。
package circuitbreaker

import (
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

// State represents the state of the circuit breaker
// State 表示熔断器的状态。
type State string

const (
	StateClosed   State = "closed"    // Normal operation | 正常运行
	StateOpen     State = "open"      // Requests are blocked | 请求被阻断
	StateHalfOpen State = "half-open" // Limited requests allowed to check recovery | 放行受限请求以检查恢复
)

// Config holds the configurable parameters
// Config 持有可配置参数。
type Config struct {
	// Failure threshold to trip the circuit | 触发熔断的失败阈值
	FailureThreshold int
	// Duration circuit stays open before allowing probes; not an execution timeout
	// 熔断打开到允许探测的时长；不是执行超时
	Timeout time.Duration
	// Success threshold to close the circuit from half-open | 半开转闭合的成功阈值
	SuccessThreshold int
	// Maximum concurrent requests allowed in half-open state | 半开状态允许的最大并发请求数
	HalfOpenMaxConcurrent int
	// Interval for resetting failure counts in closed state.
	// Zero means failures accumulate until the circuit opens.
	// 闭合状态下失败计数的重置间隔。零表示失败一直累计直到熔断打开。
	Interval time.Duration
	// Custom failure detector function (return true if response should count as failure)
	// 自定义失败判定函数（应计为失败时返回 true）
	IsFailure func(c *gin.Context, err error) bool

	// Clock reads the current time. Recovery is derived from it rather than
	// scheduled, so substituting a clock drives the circuit through every
	// state without waiting.
	//
	// Optional. Default: time.Now
	// Clock 读取当前时间。恢复由它推导而非定时调度，替换时钟即可让熔断器
	// 不等待地遍历所有状态。
	//
	// 可选。默认：time.Now
	Clock func() time.Time

	// OnOpen and OnHalfOpen answer a request refused because the circuit is
	// open, or because half-open is already at HalfOpenMaxConcurrent. None of
	// the three fires on a transition alone.
	//
	// OnClose is a notification, not a response writer: it runs after the
	// probe that closed the circuit, by which point the protected handler has
	// already answered. Record recovery here; do not write to the response or
	// advance the handler chain. Callbacks must be concurrency-safe and must
	// not retain gin.Context.
	// OnOpen 与 OnHalfOpen 应答被拒绝的请求：前者因熔断打开，后者因半开并发
	// 已达 HalfOpenMaxConcurrent。三者都不会仅因状态迁移而触发。
	//
	// OnClose 是通知，不是响应写入器：它在关闭熔断的探测之后运行，此时被保护
	// 的 handler 已完成应答。在这里记录恢复；不要写响应或推进 handler 链。
	// 回调必须并发安全，且不得保留 gin.Context。
	OnOpen     gin.HandlerFunc
	OnHalfOpen gin.HandlerFunc
	OnClose    gin.HandlerFunc
}

// DefaultConfig supplies the upstream defaults. Set Interval for a bounded
// failure-count window in production. Do not mutate this variable concurrently.
// DefaultConfig 提供上游默认值。生产环境请设置 Interval 以获得有界失败计数
// 窗口。不要并发修改此变量。
var DefaultConfig = Config{
	FailureThreshold:      5,
	Timeout:               5 * time.Second,
	SuccessThreshold:      1,
	HalfOpenMaxConcurrent: 1,
	Interval:              0,
	Clock:                 time.Now,
	IsFailure: func(c *gin.Context, err error) bool {
		return err != nil || c.Writer.Status() >= http.StatusInternalServerError
	},
	OnOpen: func(c *gin.Context) {
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
			"error": "service unavailable",
		})
	},
	OnHalfOpen: func(c *gin.Context) {
		c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
			"error": "service under recovery",
		})
	},
	OnClose: func(c *gin.Context) {
	},
}

// CircuitBreaker implements the circuit breaker pattern. Requests normally reach it
// through Middleware. Do not copy an instance after use.
// CircuitBreaker 实现熔断器模式。请求通常经 Middleware 进入。使用后不要拷贝实例。
type CircuitBreaker struct {
	failureCount     atomic.Int64 // Count of failures (atomic) | 失败计数（原子）
	successCount     atomic.Int64 // Count of successes in half-open state (atomic) | 半开状态成功计数（原子）
	totalRequests    atomic.Int64 // Count of total requests (atomic) | 总请求数（原子）
	rejectedRequests atomic.Int64 // Count of rejected requests (atomic) | 被拒绝请求数（原子）

	mutex sync.RWMutex // Protects every field below | 保护以下所有字段

	state           State
	lastStateChange time.Time
	openedAt        time.Time // with timeout, the recovery deadline | 与 timeout 一起构成恢复截止时间
	forced          bool      // ForceOpen suspends recovery until closed explicitly | ForceOpen 挂起恢复，直到显式关闭

	// gen advances on every transition. A slot is released by it, and an
	// outcome matched against it, so a request that outlived the circuit that
	// admitted it can neither free a later probe's slot nor be counted.
	// gen 随每次状态迁移递增。探测槽由它释放、结果按它匹配，因此比放行它的
	// 熔断器活得更久的请求既不能释放后续探测的槽位，也不会被计数。
	halfOpenInFlight int
	gen              uint64

	expiry time.Time // when the failure count is dropped, if Interval is set | 设置 Interval 时失败计数的清零时刻

	config                Config
	clock                 func() time.Time
	failureThreshold      int
	successThreshold      int
	halfOpenMaxConcurrent int
	timeout               time.Duration
	interval              time.Duration
}

// New initializes a circuit breaker with the given configuration
// New 按给定配置初始化熔断器。
func New(config Config) *CircuitBreaker {
	// Apply default values for zero values
	// 零值应用默认值
	if config.FailureThreshold <= 0 {
		config.FailureThreshold = DefaultConfig.FailureThreshold
	}
	if config.Timeout <= 0 {
		config.Timeout = DefaultConfig.Timeout
	}
	if config.SuccessThreshold <= 0 {
		config.SuccessThreshold = DefaultConfig.SuccessThreshold
	}
	if config.HalfOpenMaxConcurrent <= 0 {
		config.HalfOpenMaxConcurrent = DefaultConfig.HalfOpenMaxConcurrent
	}
	if config.Clock == nil {
		config.Clock = DefaultConfig.Clock
	}
	if config.IsFailure == nil {
		config.IsFailure = DefaultConfig.IsFailure
	}
	if config.OnOpen == nil {
		config.OnOpen = DefaultConfig.OnOpen
	}
	if config.OnHalfOpen == nil {
		config.OnHalfOpen = DefaultConfig.OnHalfOpen
	}
	if config.OnClose == nil {
		config.OnClose = DefaultConfig.OnClose
	}

	now := config.Clock()

	var expiry time.Time
	if config.Interval > 0 {
		expiry = now.Add(config.Interval)
	}

	return &CircuitBreaker{
		state:                 StateClosed,
		lastStateChange:       now,
		expiry:                expiry,
		config:                config,
		clock:                 config.Clock,
		failureThreshold:      config.FailureThreshold,
		successThreshold:      config.SuccessThreshold,
		halfOpenMaxConcurrent: config.HalfOpenMaxConcurrent,
		timeout:               config.Timeout,
		interval:              config.Interval,
	}
}

// Middleware adapts Gin requests to the circuit. Mount a separate circuit per
// protected route group or dependency. A nil circuit is a setup error.
// Middleware 把 Gin 请求接入熔断器。每个受保护的路由组或依赖应挂独立熔断器。
// 熔断器为 nil 属于装配错误。
func Middleware(cb *CircuitBreaker) gin.HandlerFunc {
	if cb == nil {
		panic("circuitbreaker: nil circuit")
	}
	return func(c *gin.Context) { cb.serve(c, c.Next) }
}

// Build returns the middleware for this circuit.
// Build 返回该熔断器的中间件。
func (cb *CircuitBreaker) Build() gin.HandlerFunc { return Middleware(cb) }

// admission records how a request was let in, so its outcome is applied to the
// circuit that admitted it rather than to whatever the circuit has become by
// the time it finishes.
// admission 记录请求是如何被放行的，使其结果作用于放行它的熔断器，
// 而不是请求结束时的任意状态。
type admission struct {
	state State
	gen   uint64
	// any belongs to the deprecated protocol, which reports an outcome with no
	// admission to match it against.
	// any 属于废弃协议：上报结果时没有可匹配的放行记录。
	any bool
}

// current reports whether the circuit is still the one that admitted the
// request. Every transition advances the generation, so matching it is the
// whole test - the state cannot have changed without it. An outcome that fails
// this describes a circuit that no longer exists: the request may have started
// before the circuit opened, have probed a window that has since ended, or
// have outlived a whole open-and-recover cycle whose closing probe already
// judged the dependency more recently.
// current 报告熔断器是否仍是放行该请求的那个。每次迁移都会推进 generation，
// 匹配它即是全部判据——不推进它状态就不可能变化。匹配失败的结果描述的是
// 一个已不存在的熔断器：请求可能在熔断打开前开始，探测的窗口可能已结束，
// 或者活过了整个"打开-恢复"周期，而其闭合探测已更新近地评判过依赖。
func (a admission) current(gen uint64) bool {
	return a.any || a.gen == gen
}

// serve owns admission, outcome reporting and half-open slot release.
// Panics count as failures and are rethrown to the application's Recovery
// middleware. This works whether Recovery is before or after this handler.
// serve 负责放行、结果上报与半开槽位释放。panic 计为失败并重新抛出给应用的
// Recovery 中间件；无论 Recovery 在本 handler 之前还是之后都成立。
func (cb *CircuitBreaker) serve(c *gin.Context, next func()) {
	allowed, adm, release := cb.admit(cb.clock())
	if !allowed {
		c.Abort()
		if adm.state == StateHalfOpen {
			cb.config.OnHalfOpen(c)
		} else {
			cb.config.OnOpen(c)
		}
		return
	}
	defer release()
	defer func() {
		if value := recover(); value != nil {
			cb.recordFailure(cb.clock(), adm)
			panic(value)
		}
	}()
	previousErrors := len(c.Errors)
	next()
	var failures []error
	if len(c.Errors) > previousErrors {
		for _, failure := range c.Errors[previousErrors:] {
			failures = append(failures, failure.Err)
		}
	}
	if cb.config.IsFailure(c, errors.Join(failures...)) {
		cb.recordFailure(cb.clock(), adm)
		return
	}
	if cb.recordSuccess(cb.clock(), adm) {
		cb.config.OnClose(c)
	}
}

// admit reports whether one request may proceed, how it was admitted, and the
// release for what it took - a no-op unless it took a probe slot.
//
// Closed and open both decide from the state alone, so the two that carry the
// traffic share the read lock rather than serializing every request. Only
// crossing a transition or taking a probe slot needs exclusive access.
// admit 报告单个请求能否继续、它是如何被放行的，以及其所占资源的释放函数
// ——未占探测槽时为空操作。
//
// 闭合与打开都只凭状态决定，因此承载流量的这两种状态共享读锁，而不是把
// 每个请求串行化。只有跨越状态迁移或占用探测槽才需要独占访问。
func (cb *CircuitBreaker) admit(now time.Time) (bool, admission, func()) {
	cb.totalRequests.Add(1)

	cb.mutex.RLock()
	state, gen := cb.state, cb.gen
	readOnly := state != StateHalfOpen && !cb.recoveryDueLocked(now)
	cb.mutex.RUnlock()

	if readOnly {
		return cb.admitOnState(state, gen)
	}

	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	cb.recoverLocked(now)

	if cb.state == StateHalfOpen {
		return cb.admitProbeLocked()
	}
	return cb.admitOnState(cb.state, cb.gen)
}

// admitOnState admits or refuses on the state alone, which is all a closed or
// open circuit needs, so the caller may have read that state under either lock.
// admitOnState 只凭状态决定放行或拒绝——闭合或打开的熔断器所需尽在于此，
// 因此调用者可能在任一把锁下读取该状态。
func (cb *CircuitBreaker) admitOnState(state State, gen uint64) (bool, admission, func()) {
	if state == StateOpen {
		cb.rejectedRequests.Add(1)
		return false, admission{state: StateOpen, gen: gen}, func() {}
	}
	return true, admission{state: StateClosed, gen: gen}, func() {}
}

// admitProbeLocked takes a slot of the current half-open window if one is free,
// tagged with that window so a probe outliving it cannot free a later slot.
// admitProbeLocked 在半开窗口有空位时占用一个槽位，并打上该窗口的标记，
// 使比窗口活得更久的探测无法释放后续的槽位。
func (cb *CircuitBreaker) admitProbeLocked() (bool, admission, func()) {
	if cb.halfOpenInFlight >= cb.halfOpenMaxConcurrent {
		cb.rejectedRequests.Add(1)
		return false, admission{state: StateHalfOpen}, func() {}
	}

	cb.halfOpenInFlight++
	gen := cb.gen
	return true, admission{state: StateHalfOpen, gen: gen}, func() { cb.releaseProbe(gen) }
}

// releaseProbe gives back a slot taken in generation gen. The transition that
// ends a window reclaims every slot of it, so a probe outliving its window has
// nothing to return: releasing would free a slot another probe now holds.
// releaseProbe 归还 generation gen 中占用的槽位。结束窗口的迁移会回收该窗口
// 的所有槽位，比窗口活得更久的探测已无可归还：此时释放会腾出其他探测正持有
// 的槽位。
func (cb *CircuitBreaker) releaseProbe(gen uint64) {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	if cb.gen != gen || cb.halfOpenInFlight == 0 {
		return
	}
	cb.halfOpenInFlight--
}

// recoveryDueLocked reports whether the open to half-open transition has come
// due. Either lock is enough to read it.
// recoveryDueLocked 报告"打开转半开"的迁移是否到期。任一把锁都足以读取。
func (cb *CircuitBreaker) recoveryDueLocked(now time.Time) bool {
	return cb.state == StateOpen && !cb.forced && !now.Before(cb.openedAt.Add(cb.timeout))
}

// recoverLocked moves an open circuit to half-open once its deadline has
// passed. Nothing schedules it - the first request or state read after the
// deadline applies it - so a circuit with no traffic keeps reporting open.
// recoverLocked 在截止时间过后把打开的熔断器转为半开。没有任何调度器——
// 由截止时间后的首个请求或状态读取来执行——因此无流量的熔断器会一直报告 open。
func (cb *CircuitBreaker) recoverLocked(now time.Time) {
	if !cb.recoveryDueLocked(now) {
		return
	}

	cb.state = StateHalfOpen
	cb.lastStateChange = now
	cb.failureCount.Store(0)
	cb.successCount.Store(0)
	cb.nextGenLocked()
}

// nextGenLocked starts a new generation, freeing every probe slot and leaving
// the admissions of the generation that just ended stale.
// nextGenLocked 开启新 generation，释放所有探测槽位，并使刚结束 generation
// 的放行记录全部失效。
func (cb *CircuitBreaker) nextGenLocked() {
	cb.halfOpenInFlight = 0
	cb.gen++
}

// openLocked opens the circuit on a fresh deadline; a forced open suspends
// recovery until the circuit is closed explicitly.
// openLocked 以新的截止时间打开熔断器；强制打开会挂起恢复，直到显式关闭。
func (cb *CircuitBreaker) openLocked(now time.Time, forced bool) {
	cb.state = StateOpen
	cb.openedAt = now
	cb.lastStateChange = now
	cb.forced = forced
	cb.failureCount.Store(0)
	cb.nextGenLocked()
}

// closeLocked returns the circuit to normal operation.
// closeLocked 使熔断器恢复正常运行。
func (cb *CircuitBreaker) closeLocked(now time.Time) {
	cb.state = StateClosed
	cb.lastStateChange = now
	cb.forced = false
	cb.openedAt = time.Time{}
	cb.failureCount.Store(0)
	cb.successCount.Store(0)
	if cb.interval > 0 {
		cb.expiry = now.Add(cb.interval)
	}
	cb.nextGenLocked()
}

// recordFailure counts a failure and opens the circuit if that was enough.
//
// A failure from a request the circuit has since moved past is ignored rather
// than treated as a failed probe. Ignoring it cannot make the circuit
// optimistic - only a success closes it, and those are matched the same way -
// while acting on it would end a trial that has not run and push the recovery
// deadline out again, so a backlog of stale failures could starve recovery
// indefinitely.
// recordFailure 记一次失败，达到阈值则打开熔断器。
//
// 熔断器已"越过"的请求带来的失败会被忽略，而不当作探测失败。忽略它不会让
// 熔断器变得乐观——只有成功才会闭合，且成功按同样方式匹配；若照单全收，会
// 终结一次尚未运行的试探，并把恢复截止时间再次推后，因此积压的陈旧失败可能
// 让恢复被无限期饿死。
func (cb *CircuitBreaker) recordFailure(now time.Time, adm admission) {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	cb.recoverLocked(now)

	if !adm.current(cb.gen) {
		return
	}

	switch cb.state {
	case StateHalfOpen:
		// One failure ends the trial. | 一次失败即终结试探。
		cb.openLocked(now, false)
	case StateClosed:
		cb.dropExpiredFailuresLocked(now)
		if int(cb.failureCount.Add(1)) >= cb.failureThreshold {
			cb.openLocked(now, false)
		}
	}
}

// recordSuccess counts a success and reports whether this one closed the
// circuit, so OnClose does not depend on a second state read that another
// request could win.
//
// Only a probe of the current half-open window vouches for recovery, by the
// same matching rule the failure path uses.
// recordSuccess 记一次成功，并报告是否正是这一次闭合了熔断器，使 OnClose
// 不依赖可能被其他请求抢先的第二次状态读取。
//
// 只有当前半开窗口的探测能为恢复作证，匹配规则与失败路径相同。
func (cb *CircuitBreaker) recordSuccess(now time.Time, adm admission) bool {
	// A closed circuit records no successes, so the path that carries the
	// traffic needs no more than the read lock.
	// 闭合的熔断器不记成功，因此承载流量的路径只需读锁。
	cb.mutex.RLock()
	closed := cb.state == StateClosed
	cb.mutex.RUnlock()

	if closed {
		return false
	}

	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	cb.recoverLocked(now)

	if cb.state != StateHalfOpen || !adm.current(cb.gen) {
		return false
	}
	if int(cb.successCount.Add(1)) < cb.successThreshold {
		return false
	}

	cb.closeLocked(now)
	return true
}

// dropExpiredFailuresLocked forgets accumulated failures once the Interval
// window has elapsed, the boundary instant included.
// dropExpiredFailuresLocked 在 Interval 窗口（含边界时刻）耗尽后遗忘累计的失败。
func (cb *CircuitBreaker) dropExpiredFailuresLocked(now time.Time) {
	if cb.interval <= 0 || cb.expiry.After(now) {
		return
	}
	cb.failureCount.Store(0)
	cb.expiry = now.Add(cb.interval)
}

// stateAt reports the state at now, applying a due recovery first. The common
// case does not need the write lock.
// stateAt 报告 now 时刻的状态，到期恢复会先被应用。常见情形不需要写锁。
func (cb *CircuitBreaker) stateAt(now time.Time) State {
	cb.mutex.RLock()
	state := cb.state
	due := cb.recoveryDueLocked(now)
	cb.mutex.RUnlock()

	if !due {
		return state
	}

	cb.mutex.Lock()
	defer cb.mutex.Unlock()
	cb.recoverLocked(now)
	return cb.state
}

// GetState returns the current state of the circuit breaker
// GetState 返回熔断器当前状态。
func (cb *CircuitBreaker) GetState() State {
	return cb.stateAt(cb.clock())
}

// IsOpen returns true if the circuit is open
// IsOpen 在熔断器打开时返回 true。
func (cb *CircuitBreaker) IsOpen() bool {
	return cb.GetState() == StateOpen
}

// Reset resets the circuit breaker to closed, ForceOpen included.
// Reset 将熔断器重置为闭合，ForceOpen 也不例外。
func (cb *CircuitBreaker) Reset() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	cb.closeLocked(cb.clock())
}

// ForceOpen forcibly opens the circuit regardless of failure count, and keeps
// it open: a forced-open circuit does not recover on its own when Timeout
// elapses, only when Reset or ForceClose is called.
// ForceOpen 无视失败次数强制打开熔断器并保持打开：强制打开的熔断器在
// Timeout 到期后不会自行恢复，只有调用 Reset 或 ForceClose 才会闭合。
func (cb *CircuitBreaker) ForceOpen() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	cb.openLocked(cb.clock(), true)
}

// ForceClose forcibly closes the circuit regardless of current state
// ForceClose 无视当前状态强制闭合熔断器。
func (cb *CircuitBreaker) ForceClose() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	cb.closeLocked(cb.clock())
}

// SetTimeout updates the timeout duration. A circuit that is already open
// recovers on the new deadline, since the deadline is derived, not scheduled.
// SetTimeout 更新超时时长。已打开的熔断器会按新截止时间恢复，
// 因为截止时间是推导出来的，不是调度的。
func (cb *CircuitBreaker) SetTimeout(timeout time.Duration) {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	cb.timeout = timeout
}

// Stop releases the circuit breaker's resources.
//
// Deprecated: recovery is derived from the clock rather than scheduled, so
// there is nothing to stop. This does nothing.
// Stop 释放熔断器资源。
//
// 已废弃：恢复由时钟推导而非调度，无可释放；此方法为空操作。
func (cb *CircuitBreaker) Stop() {}

// AllowRequest determines if a request is allowed based on circuit state.
//
// Deprecated: use Middleware, which admits, reports and releases as one
// operation. A slot taken here is held until ReleaseSemaphore returns it or
// the next state change reclaims it.
// AllowRequest 依据熔断器状态判断是否放行请求。
//
// 已废弃：请使用 Middleware，它把放行、上报与释放作为一次操作完成。
// 这里占用的槽位会一直持有，直到 ReleaseSemaphore 归还或下次状态迁移回收。
func (cb *CircuitBreaker) AllowRequest() (bool, State) {
	allowed, adm, _ := cb.admit(cb.clock())
	return allowed, adm.state
}

// ReleaseSemaphore releases a slot in the half-open semaphore.
//
// Deprecated: use Middleware. This releases a slot in the current half-open
// window, which is not necessarily the one it was taken in.
// ReleaseSemaphore 释放半开信号量的一个槽位。
//
// 已废弃：请使用 Middleware。此方法释放的是当前半开窗口的槽位，
// 未必是当初占用它的那个窗口。
func (cb *CircuitBreaker) ReleaseSemaphore() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	if cb.halfOpenInFlight > 0 {
		cb.halfOpenInFlight--
	}
}

// ReportSuccess increments success count and closes circuit if threshold met.
//
// Deprecated: use Middleware, which reports the outcome of the request it
// admitted and runs OnClose when that success closes the circuit.
// ReportSuccess 增加成功计数，达到阈值则闭合熔断器。
//
// 已废弃：请使用 Middleware，它上报其放行请求的结果，并在该成功闭合熔断器时
// 运行 OnClose。
func (cb *CircuitBreaker) ReportSuccess() {
	// No admission to match, so the outcome applies to whatever state is
	// current - the behaviour this method has always had.
	// 没有可匹配的放行记录，结果作用于当前任意状态——本方法一贯如此。
	cb.recordSuccess(cb.clock(), admission{any: true})
}

// ReportFailure increments failure count and opens circuit if threshold met.
//
// Deprecated: use Middleware, which reports the outcome of the request it
// admitted.
// ReportFailure 增加失败计数，达到阈值则打开熔断器。
//
// 已废弃：请使用 Middleware，它上报其放行请求的结果。
func (cb *CircuitBreaker) ReportFailure() {
	// As with ReportSuccess, there is no admission to match.
	// 与 ReportSuccess 一样，没有可匹配的放行记录。
	cb.recordFailure(cb.clock(), admission{any: true})
}

// Metrics returns basic metrics about the circuit breaker
// Metrics 返回熔断器的基础指标。
func (cb *CircuitBreaker) Metrics() gin.H {
	return gin.H{
		"state":            cb.GetState(),
		"failures":         cb.failureCount.Load(),
		"successes":        cb.successCount.Load(),
		"totalRequests":    cb.totalRequests.Load(),
		"rejectedRequests": cb.rejectedRequests.Load(),
	}
}

// GetStateStats returns detailed statistics about the circuit breaker.
//
// The state and the fields describing it are read under one lock, so a
// transition racing the read cannot pair the old state with the new
// timestamps.
// GetStateStats 返回熔断器的详细统计。
//
// 状态与描述它的字段在同一把锁下读取，因此与读取竞争的状态迁移无法把旧状态
// 与新时间戳配对。
func (cb *CircuitBreaker) GetStateStats() gin.H {
	now := cb.clock()

	cb.mutex.RLock()
	if !cb.recoveryDueLocked(now) {
		defer cb.mutex.RUnlock()
		return cb.statsLocked()
	}
	cb.mutex.RUnlock()

	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	cb.recoverLocked(now)
	return cb.statsLocked()
}

// statsLocked builds the statistics map. Caller holds either lock.
// statsLocked 构建统计映射。调用者持有任一把锁。
func (cb *CircuitBreaker) statsLocked() gin.H {
	return gin.H{
		"state":            cb.state,
		"failures":         cb.failureCount.Load(),
		"successes":        cb.successCount.Load(),
		"totalRequests":    cb.totalRequests.Load(),
		"rejectedRequests": cb.rejectedRequests.Load(),
		"lastStateChange":  cb.lastStateChange,
		"openDuration":     cb.timeout,
		"failureThreshold": cb.failureThreshold,
		"successThreshold": cb.successThreshold,
		"expiry":           cb.expiry,
	}
}

// HealthHandler reports 503 while open, and 200 otherwise. Register this
// handler outside the protected route group so an open circuit cannot hide it.
// HealthHandler 在打开时报告 503，否则 200。请把本 handler 注册在受保护路由组
// 之外，避免熔断打开时连健康检查都被挡住。
func (cb *CircuitBreaker) HealthHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		state := cb.GetState()
		data := gin.H{"state": state, "healthy": state == StateClosed}
		status := http.StatusOK
		if state == StateOpen {
			status = http.StatusServiceUnavailable
		}
		c.JSON(status, data)
	}
}
