// Package ratelimit provides per-key token-bucket rate limiting inside one
// process | ratelimit 提供单进程内按键维度的令牌桶限流.
//
// # Notes | 说明
//
// Keys default to the client IP; limiters live in an LRU [Cache] (default
// 5000 entries, 6h TTL) that callers may replace with their own concurrent
// implementation. Rejected requests get Retry-After plus RateLimit-* /
// X-RateLimit-* headers and a default JSON body, overridable via
// SetMessage / SetOnLimitReached. Refill is continuous rather than
// window-aligned, so RateLimit-Reset reports the time to refill the bucket.
// 键默认取客户端 IP；限流器存于 LRU [Cache]（默认 5000 条、6h 过期），可换成
// 调用方自己的并发实现。被拒请求带 Retry-After 与 RateLimit-* / X-RateLimit-*
// 响应头及默认 JSON 响应体，可用 SetMessage / SetOnLimitReached 覆盖。令牌
// 连续补充而非按窗口对齐，RateLimit-Reset 表示恢复满桶所需时间。
//
// # Example | 示例
//
//	rl := ratelimit.NewRateLimit(time.Minute, 100)
//	r.Use(rl.Build())
package ratelimit

import (
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"

	"github.com/apus-run/gala/components/ecache"
)

var (
	// DefaultCacheSize is the default LRU capacity for limiter entries | 限流条目默认 LRU 容量.
	DefaultCacheSize = 5000
	// DefaultTTL is the default expiry for limiter entries | 限流条目默认过期时间.
	DefaultTTL = 6 * time.Hour
)

// Cache stores limiters and must support concurrent access.
// Cache 存储限流器，必须支持并发访问。
type Cache interface {
	Add(key string, value *rate.Limiter) bool
	Get(key string) (*rate.Limiter, bool)
}

// RateLimit limits request rates per key in this process. Configure setters
// before the first Build; create a new instance to change the configuration.
// Do not copy an instance after use.
// RateLimit 在本进程内按键限制请求速率。setter 需在首次 Build 前配置；
// 修改配置请新建实例；使用后不要拷贝实例。
type RateLimit struct {
	// Time window | 时间窗口.
	window time.Duration
	// Allowed request count | 请求数量.
	requests int
	// Function producing the rate-limit key | 生成限流键的函数.
	keyFunc func(*gin.Context) string
	// Custom message in the JSON response when limited | 被限流时 JSON 响应中的自定义 message.
	message string
	// Fully custom limited response; overrides the default JSON response and message
	// | 被限流时的完全自定义响应，设置后忽略默认 JSON 响应与 message.
	onLimitReached func(*gin.Context)

	store Cache
	mu    sync.Mutex // Serializes cache misses across handlers built from this instance. | 串行化该实例各 handler 的缓存未命中。
}

// NewRateLimit creates a rate limiter | NewRateLimit 创建速率限制器。
// window: time window, e.g. 1 * time.Minute for per-minute | 时间窗口，如 1 * time.Minute 表示每分钟
// requests: allowed count, e.g. 100 for at most 100 requests | 请求数量，如 100 表示最多 100 个请求
func NewRateLimit(window time.Duration, requests int) *RateLimit {
	return &RateLimit{
		window:   window,
		requests: requests,
		keyFunc: func(ctx *gin.Context) string {
			// Default to the client IP as the rate-limit key
			// 默认使用客户端 IP 作为限流键
			return ctx.ClientIP()
		},
		store: ecache.New[string, *rate.Limiter](ecache.WithLRU(), ecache.WithMaxKeys(DefaultCacheSize), ecache.WithTTL(DefaultTTL)),
	}
}

// SetKeyFunc sets the function producing the rate-limit key
// | SetKeyFunc 设置生成限流键的函数。
func (r *RateLimit) SetKeyFunc(keyFunc func(*gin.Context) string) *RateLimit {
	r.keyFunc = keyFunc
	return r
}

// SetWindow sets the time window | SetWindow 设置时间窗口。
func (r *RateLimit) SetWindow(window time.Duration) *RateLimit {
	r.window = window
	return r
}

// SetRequests sets the allowed request count | SetRequests 设置请求数量。
func (r *RateLimit) SetRequests(requests int) *RateLimit {
	r.requests = requests
	return r
}

// SetMessage customizes the message field of the limited JSON response
// | SetMessage 自定义被限流时 JSON 响应中的 message 字段。
func (r *RateLimit) SetMessage(message string) *RateLimit {
	r.message = message
	return r
}

// SetOnLimitReached installs a fully custom limited response, overriding the
// default JSON response and SetMessage. The fn should write its own status
// code and body; Build calls c.Abort after fn returns to end the chain.
// | SetOnLimitReached 自定义被限流时的完整响应，设置后忽略默认 JSON 响应与
// SetMessage。函数内应自行写入状态码/响应体；Build 会在其返回后调用 c.Abort
// 终止链。
func (r *RateLimit) SetOnLimitReached(fn func(*gin.Context)) *RateLimit {
	r.onLimitReached = fn
	return r
}

// Build returns the gin middleware | Build 构建 gin.HandlerFunc。
// R = requests / window (req/s). Burst = requests (allows short spikes up to N).
func (r *RateLimit) Build() gin.HandlerFunc {
	store := r.store
	keyFunc := r.keyFunc
	if keyFunc == nil {
		keyFunc = func(ctx *gin.Context) string {
			return ctx.ClientIP()
		}
	}

	window, requests := r.window, r.requests
	if window <= 0 || requests <= 0 {
		panic("ratelimit: window and requests must be positive")
	}
	rateLimit := rate.Limit(float64(requests) / window.Seconds())
	burst := requests
	message := r.message
	if message == "" {
		message = "请求过于频繁，请稍后再试"
	}
	onLimitReached := r.onLimitReached

	return func(c *gin.Context) {
		key := keyFunc(c)

		lim, ok := store.Get(key)
		if !ok {
			r.mu.Lock()
			// Another request may have initialized this key while we waited.
			// 等待期间其他请求可能已初始化该 key。
			lim, ok = store.Get(key)
			if !ok {
				lim = rate.NewLimiter(rateLimit, burst)
				store.Add(key, lim)
			}
			r.mu.Unlock()
		}

		res := lim.Reserve()
		delay := res.Delay()

		if delay > 0 {
			res.Cancel()
			ra := int(math.Ceil(delay.Seconds()))
			resetAt := time.Now().Add(time.Duration(ra) * time.Second).Unix()

			c.Header("Retry-After", strconv.Itoa(ra))
			c.Header("RateLimit-Limit", strconv.Itoa(requests))
			c.Header("RateLimit-Remaining", "0")
			c.Header("RateLimit-Reset", strconv.Itoa(ra))
			c.Header("X-RateLimit-Limit", strconv.Itoa(requests))
			c.Header("X-RateLimit-Remaining", "0")
			c.Header("X-RateLimit-Reset", strconv.FormatInt(resetAt, 10))

			if onLimitReached != nil {
				onLimitReached(c)
				c.Abort()
				return
			}
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"code":    http.StatusTooManyRequests,
				"message": message,
				"data": gin.H{
					"retry_after": ra,
				},
			})
			return
		}

		remaining := lim.Tokens()
		// The token bucket refills continuously instead of resetting at window
		// end; Reset reports the time to refill the bucket.
		// 令牌桶按速率连续补充而非窗口结束一次性重置，Reset 取恢复满桶的耗时。
		refillIn := max(time.Duration((float64(requests)-remaining)*float64(window/time.Duration(requests))), 0)
		refillSecs := int(math.Ceil(refillIn.Seconds()))
		resetAt := time.Now().Add(time.Duration(refillSecs) * time.Second).Unix()

		c.Header("RateLimit-Limit", strconv.Itoa(requests))
		c.Header("RateLimit-Remaining", strconv.Itoa(int(remaining)))
		c.Header("RateLimit-Reset", strconv.Itoa(refillSecs))
		c.Header("X-RateLimit-Limit", strconv.Itoa(requests))
		c.Header("X-RateLimit-Remaining", strconv.Itoa(int(remaining)))
		c.Header("X-RateLimit-Reset", strconv.FormatInt(resetAt, 10))

		c.Next()
	}
}
