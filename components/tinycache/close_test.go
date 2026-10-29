package tinycache

import (
	"sync"
	"testing"
	"time"
)

// 并发 Close 不能 panic（重复 close channel）。
func TestCloseConcurrent(t *testing.T) {
	for range 200 {
		c := New()
		var wg sync.WaitGroup
		for range 8 {
			wg.Add(1)
			go func() { defer wg.Done(); _ = c.Close() }()
		}
		wg.Wait()
	}
}

// 非正的清理间隔不能让后台协程在 NewTicker 里 panic。
func TestNewNonPositiveCleanupInterval(t *testing.T) {
	for _, d := range []time.Duration{0, -time.Second} {
		c := New(WithCleanupInterval(d))
		time.Sleep(20 * time.Millisecond)
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
