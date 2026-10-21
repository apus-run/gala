package circuitbreaker

import (
	"testing"
	"time"

	gobreaker "github.com/sony/gobreaker/v2"
)

// These measure the raw admission protocol (Allow + done) against one shared
// breaker, mirroring the ginx middleware's bench shape: baseline, closed and
// open. ginx's bench drives whole Gin requests; numbers are not directly
// comparable across the two, only within each.
func benchAllow(b *testing.B, forceOpen bool) {
	br := New(gobreaker.Settings{Timeout: time.Hour})
	if forceOpen {
		br.ForceOpen()
	}
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			done, err := br.Allow()
			if err == nil {
				done(nil)
			}
		}
	})
}

// BenchmarkGobreakerDirect measures the unwrapped upstream breaker: the gap to
// BenchmarkBreakerClosed is this package's own overhead.
func BenchmarkGobreakerDirect(b *testing.B) {
	cb := gobreaker.NewTwoStepCircuitBreaker[struct{}](gobreaker.Settings{Timeout: time.Hour})
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			done, err := cb.Allow()
			if err == nil {
				done(nil)
			}
		}
	})
}

func BenchmarkBreakerClosed(b *testing.B) { benchAllow(b, false) }
func BenchmarkBreakerOpen(b *testing.B)   { benchAllow(b, true) }
