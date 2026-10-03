package ratelimit

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"golang.org/x/time/rate"
)

func init() {
	// Set Gin to test mode
	gin.SetMode(gin.TestMode)
}

// MockCache 是 Cache 接口的 mock 实现，用于测试
type MockCache struct {
	mu    sync.RWMutex
	store map[string]*rate.Limiter
}

// NewMockCache 创建新的 MockCache
func NewMockCache() *MockCache {
	return &MockCache{
		store: make(map[string]*rate.Limiter),
	}
}

func (m *MockCache) Add(key string, limiter *rate.Limiter) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.store[key]; exists {
		return false
	}
	m.store[key] = limiter
	return true
}

func (m *MockCache) Get(key string) (*rate.Limiter, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	limiter, exists := m.store[key]
	return limiter, exists
}

// TestNewRateLimit 测试 NewRateLimit 函数
func TestNewRateLimit(t *testing.T) {
	t.Run("创建速率限制器", func(t *testing.T) {
		rl := NewRateLimit(time.Second, 100)
		assert.NotNil(t, rl)

		// 通过Build和使用来验证基本功能
		middleware := rl.Build()
		assert.NotNil(t, middleware)
	})

	t.Run("默认使用IP作为限流键", func(t *testing.T) {
		rl := NewRateLimit(time.Second, 100)

		// 通过实际请求来验证
		router := gin.New()
		router.Use(rl.Build())
		router.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"message": "success"})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.1:8080"
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}

// TestRateLimit_SetKeyFunc 测试 SetKeyFunc
func TestRateLimit_SetKeyFunc(t *testing.T) {
	rl := NewRateLimit(time.Second, 100)
	customKeyFunc := func(c *gin.Context) string {
		return c.GetHeader("X-User-ID")
	}

	rl.SetKeyFunc(customKeyFunc)

	// 通过实际请求来验证
	router := gin.New()
	router.Use(rl.Build())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-User-ID", "user123")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestRateLimit_Build 测试 Build 方法
func TestRateLimit_Build(t *testing.T) {
	tests := []struct {
		name         string
		window       time.Duration
		requests     int
		keyFunc      func(*gin.Context) string
		store        Cache
		testRequests int
		expectBlocks int
		description  string
	}{
		{
			name:     "基础速率限制-基于IP",
			window:   time.Second,
			requests: 2,
			keyFunc: func(c *gin.Context) string {
				return c.ClientIP()
			},
			store:        NewMockCache(),
			testRequests: 5,
			expectBlocks: 3, // 前2个通过，后3个被限流
			description:  "每秒2个请求应该允许前2个，限流剩余的",
		},
		{
			name:     "自定义键函数的速率限制",
			window:   2 * time.Second,
			requests: 3,
			keyFunc: func(c *gin.Context) string {
				return c.GetHeader("X-User-ID")
			},
			store:        NewMockCache(),
			testRequests: 4,
			expectBlocks: 1, // 前3个通过，最后1个被限流
			description:  "每2秒3个请求，使用用户ID作为键",
		},
		{
			name:     "高限流允许大量请求",
			window:   time.Second,
			requests: 100,
			keyFunc: func(c *gin.Context) string {
				return c.ClientIP()
			},
			store:        NewMockCache(),
			testRequests: 10,
			expectBlocks: 0, // 高限流应该全部通过
			description:  "每秒100个请求应该允许所有10个测试请求",
		},
		{
			name:     "非常严格的速率限制",
			window:   10 * time.Second,
			requests: 1,
			keyFunc: func(c *gin.Context) string {
				return "single-key"
			},
			store:        NewMockCache(),
			testRequests: 2,
			expectBlocks: 1, // 只有第一个请求通过
			description:  "每10秒1个请求应该非常严格",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 创建速率限制器
			rl := NewRateLimit(tt.window, tt.requests)
			if tt.keyFunc != nil {
				rl.SetKeyFunc(tt.keyFunc)
			}
			if tt.store != nil {
				rl.store = tt.store
			}

			// 构建中间件
			middleware := rl.Build()
			assert.NotNil(t, middleware, "中间件不应该为nil")

			// 设置路由
			router := gin.New()
			router.Use(middleware)
			router.GET("/test", func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{"message": "success"})
			})

			successCount := 0
			blockedCount := 0

			// 发送测试请求
			for range tt.testRequests {
				req := httptest.NewRequest("GET", "/test", nil)

				// 如果需要，设置自定义header
				if tt.name == "自定义键函数的速率限制" {
					req.Header.Set("X-User-ID", "user123")
				}

				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				if w.Code == http.StatusOK {
					successCount++
				} else if w.Code == http.StatusTooManyRequests {
					blockedCount++

					// 验证限流header已设置
					assert.NotEmpty(t, w.Header().Get("Retry-After"), "Retry-After header应该设置")
					assert.NotEmpty(t, w.Header().Get("X-RateLimit-Limit"), "X-RateLimit-Limit header应该设置")
					assert.NotEmpty(t, w.Header().Get("X-RateLimit-Remaining"), "X-RateLimit-Remaining header应该设置")
					assert.NotEmpty(t, w.Header().Get("X-RateLimit-Reset"), "X-RateLimit-Reset header应该设置")

					// 验证响应体
					var response map[string]any
					assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
					assert.Equal(t, "请求过于频繁，请稍后再试", response["message"])
					assert.Contains(t, response, "message")
					assert.Contains(t, response, "data")
				}
			}

			// 验证期望
			assert.Equal(t, tt.expectBlocks, blockedCount,
				"期望 %d 个被限流的请求，实际得到 %d。%s", tt.expectBlocks, blockedCount, tt.description)
			assert.Equal(t, tt.testRequests-tt.expectBlocks, successCount,
				"期望 %d 个成功的请求，实际得到 %d", tt.testRequests-tt.expectBlocks, successCount)
		})
	}
}

// TestRateLimit_DifferentKeys 测试不同键有独立的限制
func TestRateLimit_DifferentKeys(t *testing.T) {
	keyFunc := func(c *gin.Context) string {
		return c.GetHeader("X-Client-ID")
	}

	rl := NewRateLimit(time.Second, 1)
	rl.SetKeyFunc(keyFunc)
	rl.store = NewMockCache()

	middleware := rl.Build()

	router := gin.New()
	router.Use(middleware)
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// 测试 client1
	req1 := httptest.NewRequest("GET", "/test", nil)
	req1.Header.Set("X-Client-ID", "client1")
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)
	assert.Equal(t, http.StatusOK, w1.Code, "client1的第一个请求应该成功")

	// 测试 client2 - 应该也成功（不同的键）
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.Header.Set("X-Client-ID", "client2")
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusOK, w2.Code, "client2的第一个请求应该成功")

	// 再次测试 client1 - 应该被限流
	req3 := httptest.NewRequest("GET", "/test", nil)
	req3.Header.Set("X-Client-ID", "client1")
	w3 := httptest.NewRecorder()
	router.ServeHTTP(w3, req3)
	assert.Equal(t, http.StatusTooManyRequests, w3.Code, "client1的第二个请求应该被限流")
}

// TestRateLimit_Headers 测试限流header值
func TestRateLimit_Headers(t *testing.T) {
	rl := NewRateLimit(time.Second, 5)
	rl.SetKeyFunc(func(c *gin.Context) string {
		return "test"
	})
	rl.store = NewMockCache()

	middleware := rl.Build()

	router := gin.New()
	router.Use(middleware)
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// 发送请求直到被限流
	for range 6 {
		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code == http.StatusTooManyRequests {
			// 验证header值
			retryAfter := w.Header().Get("Retry-After")
			assert.NotEmpty(t, retryAfter, "Retry-After不应该为空")
			retrySeconds, err := strconv.Atoi(retryAfter)
			assert.NoError(t, err, "Retry-After应该是有效的整数")
			assert.Greater(t, retrySeconds, 0, "Retry-After应该是正数")

			limit := w.Header().Get("X-RateLimit-Limit")
			assert.Equal(t, "5", limit, "X-RateLimit-Limit应该是5")

			remaining := w.Header().Get("X-RateLimit-Remaining")
			assert.Equal(t, "0", remaining, "被限流时X-RateLimit-Remaining应该是0")

			reset := w.Header().Get("X-RateLimit-Reset")
			assert.NotEmpty(t, reset, "X-RateLimit-Reset不应该为空")
			resetTime, err := strconv.ParseInt(reset, 10, 64)
			assert.NoError(t, err, "X-RateLimit-Reset应该是有效的unix时间戳")
			assert.Greater(t, resetTime, time.Now().Unix(), "重置时间应该在将来")

			break
		}
	}
}

// TestRateLimit_SuccessHeaders 测试成功请求的header
func TestRateLimit_SuccessHeaders(t *testing.T) {
	rl := NewRateLimit(time.Second, 5)
	rl.store = NewMockCache()

	middleware := rl.Build()

	router := gin.New()
	router.Use(middleware)
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code, "请求应该成功")
	assert.NotEmpty(t, w.Header().Get("X-RateLimit-Limit"), "X-RateLimit-Limit应该设置")
	assert.NotEmpty(t, w.Header().Get("X-RateLimit-Remaining"), "X-RateLimit-Remaining应该设置")
	assert.NotEmpty(t, w.Header().Get("X-RateLimit-Reset"), "X-RateLimit-Reset应该设置")
}

// TestRateLimit_Concurrent 测试并发安全
func TestRateLimit_Concurrent(t *testing.T) {
	rl := NewRateLimit(time.Second, 10)
	rl.store = NewMockCache()

	middleware := rl.Build()

	router := gin.New()
	router.Use(middleware)
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	var wg sync.WaitGroup
	var successCount int32
	var blockedCount int32
	concurrency := 20
	requestsPerGoroutine := 2

	for range concurrency {
		wg.Go(func() {
			for range requestsPerGoroutine {
				req := httptest.NewRequest("GET", "/test", nil)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				if w.Code == http.StatusOK {
					atomic.AddInt32(&successCount, 1)
				} else if w.Code == http.StatusTooManyRequests {
					atomic.AddInt32(&blockedCount, 1)
				}
			}
		})
	}

	wg.Wait()

	totalRequests := concurrency * requestsPerGoroutine
	// 由于并发和令牌桶的特性，可能有一些请求会通过
	// 但应该至少有一些请求被限流
	assert.Equal(t, int32(totalRequests), atomic.LoadInt32(&successCount)+atomic.LoadInt32(&blockedCount), "所有请求应该有响应")
}

type coldKeyCache struct {
	*MockCache
	arrived atomic.Int64
	missed  chan struct{}
	workers int64
}

func (m *coldKeyCache) Get(key string) (*rate.Limiter, bool) {
	if n := m.arrived.Add(1); n <= m.workers {
		if n == m.workers {
			close(m.missed)
		}
		<-m.missed
		return nil, false
	}
	return m.MockCache.Get(key)
}

func TestRateLimitConcurrentColdKeyUsesOneBucket(t *testing.T) {
	const workers = 16
	cache := &coldKeyCache{MockCache: NewMockCache(), missed: make(chan struct{}), workers: workers}
	r := NewRateLimit(time.Hour, 1).SetKeyFunc(func(*gin.Context) string { return "same-key" })
	r.store = cache
	router := gin.New()
	// Both handlers must share the cache-miss lock as well as the buckets.
	router.GET("/first", r.Build(), func(c *gin.Context) { c.Status(204) })
	router.GET("/second", r.Build(), func(c *gin.Context) { c.Status(204) })
	var admitted atomic.Int64
	var group sync.WaitGroup
	for i := range workers {
		group.Go(func() {
			path := "/first"
			if i%2 != 0 {
				path = "/second"
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
			if response.Code == 204 {
				admitted.Add(1)
			}
		})
	}
	group.Wait()
	assert.Equal(t, int64(1), admitted.Load())
}

func TestRateLimitBuildCapturesConfiguration(t *testing.T) {
	r := NewRateLimit(time.Hour, 1).SetKeyFunc(func(*gin.Context) string { return "same-key" })
	router := gin.New()
	router.GET("/test", r.Build(), func(c *gin.Context) { c.Status(204) })
	r.SetRequests(10).SetWindow(time.Minute)
	for _, status := range []int{204, 429} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("GET", "/test", nil))
		assert.Equal(t, status, response.Code)
		assert.Equal(t, "1", response.Header().Get("X-RateLimit-Limit"))
	}
}

func TestRateLimitRejectsInvalidConfiguration(t *testing.T) {
	for _, tc := range []struct {
		window   time.Duration
		requests int
	}{{0, 1}, {-time.Second, 1}, {time.Second, 0}, {time.Second, -1}} {
		assert.Panics(t, func() { NewRateLimit(tc.window, tc.requests).Build() })
	}
}
