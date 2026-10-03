package ratelimit

import (
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"

	"github.com/hashicorp/golang-lru/v2/expirable"
)

var (
	// Default LRU capacity and TTL for limiter entries.
	DefaultCacheSize = 5000
	DefaultTTL       = 6 * time.Hour
)

// Cache stores limiters and must support concurrent access.
type Cache interface {
	Add(key string, value *rate.Limiter) bool
	Get(key string) (*rate.Limiter, bool)
}

// RateLimit limits request rates per key in this process. Configure setters
// before the first Build; create a new instance to change the configuration.
// Do not copy an instance after use.
type RateLimit struct {
	// 时间窗口
	window time.Duration
	// 请求数量
	requests int
	// 生成限流键的函数
	keyFunc func(*gin.Context) string

	store Cache
	mu    sync.Mutex // Serializes cache misses across handlers built from this instance.
}

// NewRateLimit 创建速率限制器
// window: 时间窗口，如 1 * time.Minute 表示每分钟
// requests: 请求数量，如 100 表示最多100个请求
func NewRateLimit(window time.Duration, requests int) *RateLimit {
	return &RateLimit{
		window:   window,
		requests: requests,
		keyFunc: func(ctx *gin.Context) string {
			// 默认使用客户端IP作为限流键
			return ctx.ClientIP()
		},
		store: expirable.NewLRU[string, *rate.Limiter](DefaultCacheSize, nil, DefaultTTL),
	}
}

// SetKeyFunc 设置生成限流键的函数
func (r *RateLimit) SetKeyFunc(keyFunc func(*gin.Context) string) *RateLimit {
	r.keyFunc = keyFunc
	return r
}

// SetWindow 设置时间窗口
func (r *RateLimit) SetWindow(window time.Duration) *RateLimit {
	r.window = window
	return r
}

// SetRequests 设置请求数量
func (r *RateLimit) SetRequests(requests int) *RateLimit {
	r.requests = requests
	return r
}

// Build 构建 gin.HandlerFunc
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

	return func(c *gin.Context) {
		key := keyFunc(c)

		lim, ok := store.Get(key)
		if !ok {
			r.mu.Lock()
			// Another request may have initialized this key while we waited.
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
			c.Header("X-RateLimit-Limit", strconv.Itoa(requests))
			c.Header("X-RateLimit-Remaining", "0")
			c.Header("X-RateLimit-Reset", strconv.FormatInt(resetAt, 10))

			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"code":    http.StatusTooManyRequests,
				"message": "请求过于频繁，请稍后再试",
				"data": gin.H{
					"retry_after": ra,
				},
			})
			return
		}

		remaining := lim.Tokens()
		resetAt := time.Now().Add(window).Unix()

		c.Header("X-RateLimit-Limit", strconv.Itoa(requests))
		c.Header("X-RateLimit-Remaining", strconv.Itoa(int(remaining)))
		c.Header("X-RateLimit-Reset", strconv.FormatInt(resetAt, 10))

		c.Next()
	}
}
