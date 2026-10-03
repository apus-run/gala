// Copyright (c) 2021 Fiber. Adapted from gofiber/contrib/v3/circuitbreaker.
// Licensed under the MIT License; see LICENSE.

package circuitbreaker_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apus-run/gala/components/ginx/middlewares/circuitbreaker"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// fakeClock is the only source of time the circuit breaker reads when it is
// handed to Config.Clock, so advancing it drives recovery directly instead of
// standing in for a timer the test cannot reach.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// AdvanceToParity advances at least one second, stopping on a second whose
// Unix value has the requested parity. TestGetStateStatsIsNotTorn uses it to
// encode which transition a timestamp belongs to.
func (c *fakeClock) AdvanceToParity(parity int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for {
		c.t = c.t.Add(time.Second)
		if c.t.Unix()%2 == parity {
			return
		}
	}
}

const waitBound = 10 * time.Second

// newApp mounts cb in front of an "/ok" route that succeeds and a "/fail"
// route that answers 500, which the default IsFailure counts as a failure.
func newApp(cb *circuitbreaker.CircuitBreaker) *gin.Engine {
	app := gin.New()
	app.Use(circuitbreaker.Middleware(cb))
	app.GET("/ok", func(c *gin.Context) {
		c.String(http.StatusOK, "OK")
	})
	app.GET("/fail", func(c *gin.Context) {
		c.Status(http.StatusInternalServerError)
	})
	return app
}

func get(t *testing.T, app *gin.Engine, target string) *http.Response {
	t.Helper()
	response := inBackground(app, target).wait(t)
	t.Cleanup(func() { _ = response.Body.Close() })
	return response
}

// bgRequest is a request running on its own goroutine. Assertions stay on the
// test goroutine: require's FailNow is only defined there.
type bgRequest struct {
	done chan struct{}
	resp *http.Response
}

func inBackground(app *gin.Engine, target string) *bgRequest {
	r := &bgRequest{done: make(chan struct{})}
	go func() {
		defer close(r.done)
		response := httptest.NewRecorder()
		app.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
		r.resp = response.Result()
	}()
	return r
}

func (r *bgRequest) wait(t *testing.T) *http.Response {
	t.Helper()
	select {
	case <-r.done:
	case <-time.After(waitBound):
		t.Fatal("a background request never completed")
	}
	return r.resp
}

// awaitEntry waits for a handler to report that it was admitted. Bounding the
// wait keeps a refused probe a test failure rather than a hang.
func awaitEntry(t *testing.T, entered <-chan struct{}, msg string) {
	t.Helper()
	select {
	case <-entered:
	case <-time.After(waitBound):
		t.Fatal(msg)
	}
}

// releaser closes a handler's release channel exactly once, on the test's way
// out if an assertion did not get there first.
func releaser(t *testing.T, release chan struct{}) func() {
	t.Helper()
	done := sync.OnceFunc(func() { close(release) })
	t.Cleanup(done)
	return done
}

// trip drives the circuit open through the middleware.
func trip(t *testing.T, app *gin.Engine, failures int) {
	t.Helper()
	for i := 0; i < failures; i++ {
		get(t, app, "/fail")
	}
}

func TestOpensAfterFailureThreshold(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	cb := circuitbreaker.New(circuitbreaker.Config{
		FailureThreshold: 3,
		Timeout:          time.Minute,
		Clock:            clock.Now,
	})
	app := newApp(cb)

	trip(t, app, 2)
	require.Equal(t, circuitbreaker.StateClosed, cb.GetState(), "two failures of three must not open the circuit")

	trip(t, app, 1)
	require.Equal(t, circuitbreaker.StateOpen, cb.GetState())
	require.True(t, cb.IsOpen())
}

func TestOpenRejectsWithOnOpen(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	var openCalls int
	cb := circuitbreaker.New(circuitbreaker.Config{
		FailureThreshold: 1,
		Timeout:          time.Minute,
		Clock:            clock.Now,
		OnOpen: func(c *gin.Context) {
			openCalls++
			c.Status(http.StatusServiceUnavailable)
		},
	})
	app := newApp(cb)

	trip(t, app, 1)
	require.Zero(t, openCalls, "OnOpen answers a refused request; it does not fire when the circuit opens")

	resp := get(t, app, "/ok")
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	require.Equal(t, 1, openCalls)
}

func TestRecoversToHalfOpenOnTheClock(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	cb := circuitbreaker.New(circuitbreaker.Config{
		FailureThreshold: 1,
		Timeout:          30 * time.Second,
		Clock:            clock.Now,
	})
	app := newApp(cb)

	trip(t, app, 1)
	require.Equal(t, circuitbreaker.StateOpen, cb.GetState())

	clock.Advance(29 * time.Second)
	require.Equal(t, circuitbreaker.StateOpen, cb.GetState(), "the circuit must stay open until the timeout elapses")

	clock.Advance(time.Second)
	require.Equal(t, circuitbreaker.StateHalfOpen, cb.GetState(), "the boundary instant counts as elapsed")
}

func TestHalfOpenProbeClosesTheCircuit(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	var closeCalls int
	cb := circuitbreaker.New(circuitbreaker.Config{
		FailureThreshold: 1,
		SuccessThreshold: 2,
		Timeout:          time.Minute,
		Clock:            clock.Now,
		OnClose: func(c *gin.Context) {
			closeCalls++
		},
	})
	app := newApp(cb)

	trip(t, app, 1)
	clock.Advance(time.Minute)

	resp := get(t, app, "/ok")
	require.Equal(t, http.StatusOK, resp.StatusCode, "a half-open probe reaches the handler")
	require.Equal(t, circuitbreaker.StateHalfOpen, cb.GetState(), "one success of two keeps the trial open")
	require.Zero(t, closeCalls)

	resp = get(t, app, "/ok")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, circuitbreaker.StateClosed, cb.GetState())
	require.Equal(t, 1, closeCalls, "OnClose runs for the probe that closed the circuit")

	// OnClose runs after the handler has answered, so a callback that writes
	// nothing must leave that answer untouched.
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "OK", string(body), "the closing probe keeps the protected handler's response")
}

func TestHalfOpenFailureReopens(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	cb := circuitbreaker.New(circuitbreaker.Config{
		FailureThreshold: 5,
		Timeout:          time.Minute,
		Clock:            clock.Now,
	})
	app := newApp(cb)

	trip(t, app, 5)
	clock.Advance(time.Minute)
	require.Equal(t, circuitbreaker.StateHalfOpen, cb.GetState())

	get(t, app, "/fail")
	require.Equal(t, circuitbreaker.StateOpen, cb.GetState(), "one failed probe ends the trial regardless of the threshold")

	clock.Advance(time.Minute)
	require.Equal(t, circuitbreaker.StateHalfOpen, cb.GetState(), "reopening starts a fresh recovery deadline")
}

func TestHalfOpenAdmitsAtMostMaxConcurrent(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	var halfOpenCalls int64
	cb := circuitbreaker.New(circuitbreaker.Config{
		FailureThreshold:      1,
		SuccessThreshold:      99, // never close, so the window stays open
		HalfOpenMaxConcurrent: 2,
		Timeout:               time.Minute,
		Clock:                 clock.Now,
		OnHalfOpen: func(c *gin.Context) {
			atomic.AddInt64(&halfOpenCalls, 1)
			c.Status(http.StatusTooManyRequests)
		},
	})

	entered := make(chan struct{}, 8)
	release := make(chan struct{})
	app := gin.New()
	app.Use(circuitbreaker.Middleware(cb))
	app.GET("/block", func(c *gin.Context) {
		entered <- struct{}{}
		<-release
		c.String(http.StatusOK, "OK")
	})
	app.GET("/fail", func(c *gin.Context) {
		c.Status(http.StatusInternalServerError)
	})

	trip(t, app, 1)
	clock.Advance(time.Minute)

	releaseAll := releaser(t, release)

	// Fill both slots and keep them held.
	held := []*bgRequest{inBackground(app, "/block"), inBackground(app, "/block")}
	awaitEntry(t, entered, "the first probe of two was not admitted")
	awaitEntry(t, entered, "the second probe of two was not admitted")

	resp := get(t, app, "/block")
	require.Equal(t, http.StatusTooManyRequests, resp.StatusCode, "a third concurrent probe is refused")
	require.Equal(t, int64(1), atomic.LoadInt64(&halfOpenCalls))

	releaseAll()
	for _, r := range held {
		r.wait(t)
	}
}

// TestHalfOpenSlotsAreNotLeakedAcrossWindows pins the guarantee that a probe
// which outlives its half-open window cannot free a slot in a later one.
//
// The stale release has to land while the new window is already at capacity,
// which is the only moment the two behaviours differ: a release that ignores
// which window its slot came from decrements the new window's count and lets
// an extra probe in.
func TestHalfOpenSlotsAreNotLeakedAcrossWindows(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	cb := circuitbreaker.New(circuitbreaker.Config{
		FailureThreshold:      1,
		SuccessThreshold:      99, // never close, so a window ends only by failing
		HalfOpenMaxConcurrent: 2,
		Timeout:               time.Minute,
		Clock:                 clock.Now,
	})

	strandedIn := make(chan struct{}, 1)
	strandedOut := make(chan struct{})
	filledIn := make(chan struct{}, 4)
	filledOut := make(chan struct{})

	app := gin.New()
	app.Use(circuitbreaker.Middleware(cb))
	app.GET("/stranded", func(c *gin.Context) {
		strandedIn <- struct{}{}
		<-strandedOut
		c.String(http.StatusOK, "OK")
	})
	app.GET("/hold", func(c *gin.Context) {
		filledIn <- struct{}{}
		<-filledOut
		c.String(http.StatusOK, "OK")
	})
	app.GET("/probe", func(c *gin.Context) {
		c.String(http.StatusOK, "OK")
	})
	app.GET("/fail", func(c *gin.Context) {
		c.Status(http.StatusInternalServerError)
	})

	releaseStranded := releaser(t, strandedOut)
	releaseFilled := releaser(t, filledOut)

	trip(t, app, 1)
	clock.Advance(time.Minute)
	require.Equal(t, circuitbreaker.StateHalfOpen, cb.GetState())

	// A probe takes a slot in this window and stays in flight.
	stranded := inBackground(app, "/stranded")
	awaitEntry(t, strandedIn, "the stranded probe was not admitted")

	// End the window underneath it: the second slot is free, so this failing
	// probe is admitted and reopens the circuit.
	get(t, app, "/fail")
	require.Equal(t, circuitbreaker.StateOpen, cb.GetState())

	// A new window, filled to capacity by two fresh probes.
	clock.Advance(time.Minute)
	require.Equal(t, circuitbreaker.StateHalfOpen, cb.GetState())
	filled := []*bgRequest{inBackground(app, "/hold"), inBackground(app, "/hold")}
	awaitEntry(t, filledIn, "the new half-open window admitted fewer probes than HalfOpenMaxConcurrent")
	awaitEntry(t, filledIn, "the new half-open window admitted fewer probes than HalfOpenMaxConcurrent")

	// Now let the stranded probe finish. Its slot belonged to the window that
	// has already ended, so returning it must not free one of these two.
	releaseStranded()
	stranded.wait(t)

	resp := get(t, app, "/probe")
	require.Equal(t, http.StatusTooManyRequests, resp.StatusCode,
		"a slot from the previous half-open window was returned into this one, admitting more probes than HalfOpenMaxConcurrent")

	releaseFilled()
	for _, r := range filled {
		r.wait(t)
	}
}

func TestForceOpenIsSticky(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	cb := circuitbreaker.New(circuitbreaker.Config{
		FailureThreshold: 5,
		Timeout:          time.Minute,
		Clock:            clock.Now,
	})
	app := newApp(cb)

	cb.ForceOpen()
	require.Equal(t, circuitbreaker.StateOpen, cb.GetState())

	clock.Advance(10 * time.Minute)
	require.Equal(t, circuitbreaker.StateOpen, cb.GetState(), "a forced-open circuit does not recover on the timeout")

	resp := get(t, app, "/ok")
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)

	cb.Reset()
	require.Equal(t, circuitbreaker.StateClosed, cb.GetState(), "only an explicit close ends a forced open")
}

func TestForceOpenOverridesAnOpenCircuitsRecovery(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	cb := circuitbreaker.New(circuitbreaker.Config{
		FailureThreshold: 1,
		Timeout:          time.Minute,
		Clock:            clock.Now,
	})
	app := newApp(cb)

	trip(t, app, 1)
	cb.ForceOpen() // already open, but now pinned

	clock.Advance(10 * time.Minute)
	require.Equal(t, circuitbreaker.StateOpen, cb.GetState())
}

func TestForceCloseAndReset(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		close func(*circuitbreaker.CircuitBreaker)
	}{
		{"ForceClose", func(cb *circuitbreaker.CircuitBreaker) { cb.ForceClose() }},
		{"Reset", func(cb *circuitbreaker.CircuitBreaker) { cb.Reset() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			clock := newFakeClock()
			cb := circuitbreaker.New(circuitbreaker.Config{
				FailureThreshold: 2,
				Interval:         10 * time.Second,
				Timeout:          time.Minute,
				Clock:            clock.Now,
			})
			app := newApp(cb)

			trip(t, app, 2)
			require.Equal(t, circuitbreaker.StateOpen, cb.GetState())

			clock.Advance(5 * time.Second)
			tc.close(cb)

			require.Equal(t, circuitbreaker.StateClosed, cb.GetState())

			stats := cb.GetStateStats()
			require.Equal(t, int64(0), stats["failures"])
			require.Equal(t, int64(0), stats["successes"])
			require.Equal(t, clock.Now(), stats["lastStateChange"])
			require.Equal(t, clock.Now().Add(10*time.Second), stats["expiry"], "closing starts a new failure-count window")

			resp := get(t, app, "/ok")
			require.Equal(t, http.StatusOK, resp.StatusCode, "a closed circuit passes requests through")
		})
	}
}

func TestSetTimeoutAppliesToAnOpenCircuit(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	cb := circuitbreaker.New(circuitbreaker.Config{
		FailureThreshold: 1,
		Timeout:          time.Hour,
		Clock:            clock.Now,
	})
	app := newApp(cb)

	trip(t, app, 1)
	clock.Advance(time.Minute)
	require.Equal(t, circuitbreaker.StateOpen, cb.GetState())

	// The deadline is derived from the timeout, so shortening it recovers a
	// circuit that is already open.
	cb.SetTimeout(30 * time.Second)
	require.Equal(t, circuitbreaker.StateHalfOpen, cb.GetState())
}

func TestIntervalDropsFailuresBetweenWindows(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	cb := circuitbreaker.New(circuitbreaker.Config{
		FailureThreshold: 3,
		Interval:         10 * time.Second,
		Timeout:          time.Minute,
		Clock:            clock.Now,
	})
	app := newApp(cb)

	require.Equal(t, clock.Now().Add(10*time.Second), cb.GetStateStats()["expiry"],
		"New derives the first window from the configured clock")

	trip(t, app, 2)
	require.Equal(t, int64(2), cb.Metrics()["failures"])

	// The boundary instant counts as elapsed, so the next failure starts over.
	clock.Advance(10 * time.Second)
	trip(t, app, 1)
	require.Equal(t, int64(1), cb.Metrics()["failures"], "the window elapsed, so the earlier failures are forgotten")
	require.Equal(t, circuitbreaker.StateClosed, cb.GetState())

	trip(t, app, 2)
	require.Equal(t, circuitbreaker.StateOpen, cb.GetState(), "three failures inside one window still open the circuit")
}

func TestIntervalWindowIsNotDroppedEarly(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	cb := circuitbreaker.New(circuitbreaker.Config{
		FailureThreshold: 3,
		Interval:         10 * time.Second,
		Timeout:          time.Minute,
		Clock:            clock.Now,
	})
	app := newApp(cb)

	trip(t, app, 2)
	clock.Advance(9 * time.Second)
	trip(t, app, 1)

	require.Equal(t, circuitbreaker.StateOpen, cb.GetState(), "failures inside the window accumulate")
}

func TestCustomFailureDetection(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	// Count 404 as a failure and ignore 500, the opposite of the default.
	cb := circuitbreaker.New(circuitbreaker.Config{
		FailureThreshold: 2,
		Timeout:          time.Minute,
		Clock:            clock.Now,
		IsFailure: func(c *gin.Context, err error) bool {
			return c.Writer.Status() == http.StatusNotFound
		},
	})

	app := gin.New()
	app.Use(circuitbreaker.Middleware(cb))
	app.GET("/missing", func(c *gin.Context) {
		c.Status(http.StatusNotFound)
	})
	app.GET("/fail", func(c *gin.Context) {
		c.Status(http.StatusInternalServerError)
	})

	trip(t, app, 5)
	require.Equal(t, circuitbreaker.StateClosed, cb.GetState(), "500 is not a failure under this detector")

	for i := 0; i < 2; i++ {
		get(t, app, "/missing")
	}
	require.Equal(t, circuitbreaker.StateOpen, cb.GetState(), "404 is a failure under this detector")
}

// TestDefaultOnCloseDoesNotAdvanceChainTwice verifies that adapting the upstream
// notification does not run any Gin handler more than once.
func TestDefaultOnCloseDoesNotAdvanceChainTwice(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	cb := circuitbreaker.New(circuitbreaker.Config{
		FailureThreshold: 1,
		SuccessThreshold: 1,
		Timeout:          time.Minute,
		Clock:            clock.Now,
		// OnClose deliberately left nil so New installs the default.
	})

	var protectedCalls int
	app := gin.New()
	app.Use(circuitbreaker.Middleware(cb))
	app.GET("/fail", func(c *gin.Context) {
		c.Status(http.StatusInternalServerError)
	})
	app.GET("/ok", func(c *gin.Context) {
		c.String(http.StatusOK, "OK")
	}, func(c *gin.Context) {
		protectedCalls++
		c.String(http.StatusOK, "protected")
	})

	trip(t, app, 1)
	clock.Advance(time.Minute)

	resp := get(t, app, "/ok")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, circuitbreaker.StateClosed, cb.GetState(), "the probe closed the circuit, so OnClose ran")
	require.Equal(t, 1, protectedCalls, "Gin's handler chain must run exactly once")
}

func TestHealthHandler(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	cb := circuitbreaker.New(circuitbreaker.Config{
		FailureThreshold: 1,
		Timeout:          time.Minute,
		Clock:            clock.Now,
	})

	app := gin.New()
	app.GET("/health", cb.HealthHandler())
	protected := gin.New()
	protected.Use(circuitbreaker.Middleware(cb))
	protected.GET("/fail", func(c *gin.Context) {
		c.Status(http.StatusInternalServerError)
	})

	resp := get(t, app, "/health")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, resp.Header.Get("Content-Type"), "application/json")

	trip(t, protected, 1)

	resp = get(t, app, "/health")
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)

	// Recovery is visible through the health endpoint without any traffic.
	clock.Advance(time.Minute)
	resp = get(t, app, "/health")
	require.Equal(t, http.StatusOK, resp.StatusCode, "half-open is not unhealthy")
}

func TestMetricsCountRequestsAndRejections(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	cb := circuitbreaker.New(circuitbreaker.Config{
		FailureThreshold: 1,
		Timeout:          time.Minute,
		Clock:            clock.Now,
	})
	app := newApp(cb)

	trip(t, app, 1) // one admitted request that failed
	get(t, app, "/ok")
	get(t, app, "/ok") // two refused

	metrics := cb.Metrics()
	require.Equal(t, circuitbreaker.StateOpen, metrics["state"])
	require.Equal(t, int64(3), metrics["totalRequests"])
	require.Equal(t, int64(2), metrics["rejectedRequests"])
}

func TestConcurrentFailuresOpenOnce(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	cb := circuitbreaker.New(circuitbreaker.Config{
		FailureThreshold: 5,
		Timeout:          time.Minute,
		Clock:            clock.Now,
	})
	app := newApp(cb)

	fired := make([]*bgRequest, 0, 20)
	for i := 0; i < 20; i++ {
		fired = append(fired, inBackground(app, "/fail"))
	}
	for _, r := range fired {
		r.wait(t)
	}

	require.Equal(t, circuitbreaker.StateOpen, cb.GetState())
	require.Equal(t, int64(20), cb.Metrics()["totalRequests"])
}

func TestConcurrentProbesCloseTheCircuitOnce(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	var closeCalls int64
	cb := circuitbreaker.New(circuitbreaker.Config{
		FailureThreshold:      1,
		SuccessThreshold:      2,
		HalfOpenMaxConcurrent: 4,
		Timeout:               time.Minute,
		Clock:                 clock.Now,
		OnClose: func(c *gin.Context) {
			atomic.AddInt64(&closeCalls, 1)
		},
	})
	app := newApp(cb)

	trip(t, app, 1)
	clock.Advance(time.Minute)

	fired := make([]*bgRequest, 0, 20)
	for i := 0; i < 20; i++ {
		fired = append(fired, inBackground(app, "/ok"))
	}
	for _, r := range fired {
		r.wait(t)
	}

	require.Equal(t, circuitbreaker.StateClosed, cb.GetState())
	require.Equal(t, int64(1), atomic.LoadInt64(&closeCalls), "only the probe that crossed the threshold closes the circuit")
}

// The deprecated protocol keeps working for callers that have not moved to
// Middleware yet.
func TestDeprecatedProtocolStillDrivesTheCircuit(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	cb := circuitbreaker.New(circuitbreaker.Config{
		FailureThreshold:      2,
		SuccessThreshold:      1,
		HalfOpenMaxConcurrent: 1,
		Timeout:               time.Minute,
		Clock:                 clock.Now,
	})

	allowed, state := cb.AllowRequest()
	require.True(t, allowed)
	require.Equal(t, circuitbreaker.StateClosed, state)

	cb.ReportFailure()
	cb.ReportFailure()
	require.Equal(t, circuitbreaker.StateOpen, cb.GetState())

	allowed, state = cb.AllowRequest()
	require.False(t, allowed)
	require.Equal(t, circuitbreaker.StateOpen, state)

	clock.Advance(time.Minute)

	allowed, state = cb.AllowRequest()
	require.True(t, allowed, "the one half-open slot is available")
	require.Equal(t, circuitbreaker.StateHalfOpen, state)

	allowed, _ = cb.AllowRequest()
	require.False(t, allowed, "the slot is still held until it is released")

	cb.ReleaseSemaphore()
	allowed, _ = cb.AllowRequest()
	require.True(t, allowed, "releasing the slot admits the next probe")

	cb.ReportSuccess()
	require.Equal(t, circuitbreaker.StateClosed, cb.GetState())
}

func TestStopIsANoOp(t *testing.T) {
	t.Parallel()

	cb := circuitbreaker.New(circuitbreaker.Config{Clock: newFakeClock().Now})
	cb.Stop()
	require.Equal(t, circuitbreaker.StateClosed, cb.GetState(), "Stop leaves a usable circuit breaker")
}

func TestDefaultsAreApplied(t *testing.T) {
	t.Parallel()

	cb := circuitbreaker.New(circuitbreaker.Config{})
	stats := cb.GetStateStats()

	require.Equal(t, circuitbreaker.DefaultConfig.FailureThreshold, stats["failureThreshold"])
	require.Equal(t, circuitbreaker.DefaultConfig.SuccessThreshold, stats["successThreshold"])
	require.Equal(t, circuitbreaker.DefaultConfig.Timeout, stats["openDuration"])
	require.Equal(t, circuitbreaker.StateClosed, stats["state"])
	require.True(t, stats["expiry"].(time.Time).IsZero(), "a zero Interval sets no window")
}

// TestGetStateStatsIsNotTorn pins that the reported state and the timestamps
// describing it come from the same moment.
//
// The gap between reading the state and reading its metadata cannot be entered
// on demand through the interface, so the test makes a torn pairing detectable
// instead: every transition to open lands on an even second and every
// transition to closed on an odd one, so "closed" reported with an even
// lastStateChange is proof the two halves came from different moments.
func TestGetStateStatsIsNotTorn(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	cb := circuitbreaker.New(circuitbreaker.Config{
		FailureThreshold: 1,
		Timeout:          time.Hour, // recovery must not move the state here
		Clock:            clock.Now,
	})
	app := newApp(cb)

	// The circuit starts closed at the clock's base instant, whose parity is
	// arbitrary; close it once on an odd second so the starting pairing obeys
	// the rule the reader checks.
	clock.AdvanceToParity(1)
	cb.ForceClose()

	const rounds = 400
	torn := make(chan string, 1)
	stop := make(chan struct{})
	readerDone := make(chan struct{})

	go func() {
		defer close(readerDone)
		for {
			select {
			case <-stop:
				return
			default:
			}

			stats := cb.GetStateStats()
			state, _ := stats["state"].(circuitbreaker.State)
			changed, _ := stats["lastStateChange"].(time.Time)
			parity := changed.Unix() % 2

			switch state {
			case circuitbreaker.StateOpen:
				if parity != 0 {
					select {
					case torn <- "open reported with a lastStateChange from a close":
					default:
					}
					return
				}
			case circuitbreaker.StateClosed:
				if parity != 1 {
					select {
					case torn <- "closed reported with a lastStateChange from an open":
					default:
					}
					return
				}
			}
		}
	}()

	for i := 0; i < rounds; i++ {
		clock.AdvanceToParity(0)
		get(t, app, "/fail") // opens on an even second
		clock.AdvanceToParity(1)
		cb.ForceClose() // closes on an odd second
	}

	close(stop)
	<-readerDone

	select {
	case msg := <-torn:
		t.Fatalf("GetStateStats returned a torn snapshot: %s", msg)
	default:
	}
}

// TestGetStateStatsSettlesDueRecovery pins that reading the stats applies a
// recovery that has come due, and reports it with the timestamps that belong
// to it - the branch where the read has to take the write lock.
func TestGetStateStatsSettlesDueRecovery(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	cb := circuitbreaker.New(circuitbreaker.Config{
		FailureThreshold: 1,
		Interval:         time.Minute,
		Timeout:          30 * time.Second,
		Clock:            clock.Now,
	})
	app := newApp(cb)

	trip(t, app, 1)
	openedAt := clock.Now()
	require.Equal(t, circuitbreaker.StateOpen, cb.GetStateStats()["state"])
	require.Equal(t, openedAt, cb.GetStateStats()["lastStateChange"])

	clock.Advance(30 * time.Second)
	recoveredAt := clock.Now()

	stats := cb.GetStateStats()
	require.Equal(t, circuitbreaker.StateHalfOpen, stats["state"], "the stats read settles a due recovery")
	require.Equal(t, recoveredAt, stats["lastStateChange"], "and reports the moment it happened")
	require.Equal(t, 30*time.Second, stats["openDuration"])
	require.Equal(t, int64(0), stats["failures"], "entering half-open clears the counters")
}

// TestStaleSuccessDoesNotCloseTheCircuit pins that only a probe from the
// current half-open window can vouch for recovery.
//
// A request admitted while the circuit was closed may still be in flight when
// other traffic opens the circuit and the recovery deadline passes. Its
// success says nothing about the state of the dependency now, so it must
// neither close the circuit nor fire OnClose.
func TestStaleSuccessDoesNotCloseTheCircuit(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	var closeCalls int64
	cb := circuitbreaker.New(circuitbreaker.Config{
		FailureThreshold: 1,
		SuccessThreshold: 1,
		Timeout:          time.Minute,
		Clock:            clock.Now,
		OnClose: func(c *gin.Context) {
			atomic.AddInt64(&closeCalls, 1)
		},
	})

	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	releaseSlow := releaser(t, release)

	app := gin.New()
	app.Use(circuitbreaker.Middleware(cb))
	app.GET("/slow", func(c *gin.Context) {
		entered <- struct{}{}
		<-release
		c.String(http.StatusOK, "OK")
	})
	app.GET("/ok", func(c *gin.Context) {
		c.String(http.StatusOK, "OK")
	})
	app.GET("/fail", func(c *gin.Context) {
		c.Status(http.StatusInternalServerError)
	})

	// Admitted while closed, and still running for the rest of the test.
	slow := inBackground(app, "/slow")
	awaitEntry(t, entered, "the slow request was not admitted while closed")

	// Other traffic opens the circuit underneath it, and the deadline passes.
	get(t, app, "/fail")
	require.Equal(t, circuitbreaker.StateOpen, cb.GetState())
	clock.Advance(time.Minute)

	// Its success must not be mistaken for a recovery probe.
	releaseSlow()
	require.Equal(t, http.StatusOK, slow.wait(t).StatusCode)

	require.Equal(t, circuitbreaker.StateHalfOpen, cb.GetState(),
		"a success from a request admitted while closed must not close the circuit")
	require.Equal(t, int64(0), atomic.LoadInt64(&closeCalls),
		"OnClose must not fire for a request that was never a half-open probe")

	// A genuine probe still closes it.
	require.Equal(t, http.StatusOK, get(t, app, "/ok").StatusCode)
	require.Equal(t, circuitbreaker.StateClosed, cb.GetState())
	require.Equal(t, int64(1), atomic.LoadInt64(&closeCalls))
}

// TestProbeFromAnEndedWindowDoesNotCloseTheCircuit is the other half of
// matching a success to its admission: a probe admitted to one half-open
// window can still be running when that window ends and a later one begins.
// It was testing a state the circuit has already left, so its success must not
// close the new window.
func TestProbeFromAnEndedWindowDoesNotCloseTheCircuit(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	var closeCalls int64
	cb := circuitbreaker.New(circuitbreaker.Config{
		FailureThreshold:      1,
		SuccessThreshold:      1,
		HalfOpenMaxConcurrent: 2, // room for the stranded probe and the one that ends the window
		Timeout:               time.Minute,
		Clock:                 clock.Now,
		OnClose: func(c *gin.Context) {
			atomic.AddInt64(&closeCalls, 1)
		},
	})

	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	releaseStranded := releaser(t, release)

	app := gin.New()
	app.Use(circuitbreaker.Middleware(cb))
	app.GET("/stranded", func(c *gin.Context) {
		entered <- struct{}{}
		<-release
		c.String(http.StatusOK, "OK")
	})
	app.GET("/ok", func(c *gin.Context) {
		c.String(http.StatusOK, "OK")
	})
	app.GET("/fail", func(c *gin.Context) {
		c.Status(http.StatusInternalServerError)
	})

	trip(t, app, 1)
	clock.Advance(time.Minute)
	require.Equal(t, circuitbreaker.StateHalfOpen, cb.GetState())

	// A probe of this window that stays in flight.
	stranded := inBackground(app, "/stranded")
	awaitEntry(t, entered, "the stranded probe was not admitted")

	// End the window, then open a fresh one.
	get(t, app, "/fail")
	require.Equal(t, circuitbreaker.StateOpen, cb.GetState())
	clock.Advance(time.Minute)
	require.Equal(t, circuitbreaker.StateHalfOpen, cb.GetState())

	// The stranded probe belongs to the window that has ended.
	releaseStranded()
	require.Equal(t, http.StatusOK, stranded.wait(t).StatusCode)

	require.Equal(t, circuitbreaker.StateHalfOpen, cb.GetState(),
		"a probe from an ended window must not close the window that replaced it")
	require.Equal(t, int64(0), atomic.LoadInt64(&closeCalls))

	// A probe of the current window still closes it.
	require.Equal(t, http.StatusOK, get(t, app, "/ok").StatusCode)
	require.Equal(t, circuitbreaker.StateClosed, cb.GetState())
	require.Equal(t, int64(1), atomic.LoadInt64(&closeCalls))
}

// TestStaleFailureDoesNotReopenTheCircuit is the failure-path mirror of
// TestStaleSuccessDoesNotCloseTheCircuit. A request admitted while the circuit
// was closed may still be in flight when other traffic opens the circuit and
// the recovery deadline passes. Its failure describes a circuit that no longer
// exists, so it must not abort the half-open trial before a real probe runs.
func TestStaleFailureDoesNotReopenTheCircuit(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	cb := circuitbreaker.New(circuitbreaker.Config{
		FailureThreshold: 1,
		SuccessThreshold: 1,
		Timeout:          time.Minute,
		Clock:            clock.Now,
	})

	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	releaseSlow := releaser(t, release)

	app := gin.New()
	app.Use(circuitbreaker.Middleware(cb))
	app.GET("/slowfail", func(c *gin.Context) {
		entered <- struct{}{}
		<-release
		c.Status(http.StatusInternalServerError)
	})
	app.GET("/ok", func(c *gin.Context) {
		c.String(http.StatusOK, "OK")
	})
	app.GET("/fail", func(c *gin.Context) {
		c.Status(http.StatusInternalServerError)
	})

	// Admitted while closed, and still running for the rest of the test.
	slow := inBackground(app, "/slowfail")
	awaitEntry(t, entered, "the slow request was not admitted while closed")

	// Other traffic opens the circuit underneath it, and the deadline passes.
	get(t, app, "/fail")
	require.Equal(t, circuitbreaker.StateOpen, cb.GetState())
	clock.Advance(time.Minute)

	// Its failure must not end a trial that has not run.
	releaseSlow()
	slow.wait(t)

	require.Equal(t, circuitbreaker.StateHalfOpen, cb.GetState(),
		"a failure from a request admitted while closed must not reopen the circuit")

	// The trial is still available, so a genuine probe decides.
	require.Equal(t, http.StatusOK, get(t, app, "/ok").StatusCode)
	require.Equal(t, circuitbreaker.StateClosed, cb.GetState())
}

// An outcome can outlive not just the state that admitted it but a whole
// open-and-recover cycle, arriving back in a closed circuit that looks like the
// one it left. The probe that closed that circuit judged the dependency more
// recently, so a failure from before the outage must not undo it - otherwise
// every recovery can be bounced straight back open by the backlog behind it.
func TestStaleFailureCannotReopenARecoveredCircuit(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	cb := circuitbreaker.New(circuitbreaker.Config{
		FailureThreshold: 1,
		SuccessThreshold: 1,
		Timeout:          time.Minute,
		Clock:            clock.Now,
	})

	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	releaseSlow := releaser(t, release)

	app := gin.New()
	app.Use(circuitbreaker.Middleware(cb))
	app.GET("/slow", func(c *gin.Context) {
		entered <- struct{}{}
		<-release
		c.Status(http.StatusInternalServerError)
	})
	app.GET("/ok", func(c *gin.Context) {
		c.String(http.StatusOK, "OK")
	})
	app.GET("/fail", func(c *gin.Context) {
		c.Status(http.StatusInternalServerError)
	})

	// Admitted while closed, and still running for the rest of the test.
	slow := inBackground(app, "/slow")
	awaitEntry(t, entered, "the slow request was not admitted while closed")

	// The circuit opens, comes due, and a genuine probe closes it again.
	get(t, app, "/fail")
	require.Equal(t, circuitbreaker.StateOpen, cb.GetState())
	clock.Advance(time.Minute)
	require.Equal(t, http.StatusOK, get(t, app, "/ok").StatusCode)
	require.Equal(t, circuitbreaker.StateClosed, cb.GetState(), "a probe closed the circuit")

	// Only now does the request admitted before any of that fail.
	releaseSlow()
	slow.wait(t)

	require.Equal(t, circuitbreaker.StateClosed, cb.GetState(),
		"a failure admitted before the circuit opened must not reopen it after a probe recovered it")
	require.Equal(t, http.StatusOK, get(t, app, "/ok").StatusCode,
		"the recovered circuit still serves traffic")
}

// TestStaleFailuresCannotStarveRecovery is why the failure path matters more
// than symmetry. Each stale failure that reopens the circuit also resets the
// deadline, so a backlog of them can hold the circuit open indefinitely and
// never let a probe through.
func TestStaleFailuresCannotStarveRecovery(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	cb := circuitbreaker.New(circuitbreaker.Config{
		FailureThreshold: 1,
		SuccessThreshold: 1,
		Timeout:          time.Minute,
		Clock:            clock.Now,
	})

	const backlog = 5
	entered := make(chan struct{}, backlog)
	release := make(chan struct{})
	releaseAll := releaser(t, release)

	app := gin.New()
	app.Use(circuitbreaker.Middleware(cb))
	app.GET("/slowfail", func(c *gin.Context) {
		entered <- struct{}{}
		<-release
		c.Status(http.StatusInternalServerError)
	})
	app.GET("/ok", func(c *gin.Context) {
		c.String(http.StatusOK, "OK")
	})
	app.GET("/fail", func(c *gin.Context) {
		c.Status(http.StatusInternalServerError)
	})

	// A backlog of requests admitted while closed, all still in flight.
	stale := make([]*bgRequest, 0, backlog)
	for i := 0; i < backlog; i++ {
		stale = append(stale, inBackground(app, "/slowfail"))
	}
	for i := 0; i < backlog; i++ {
		awaitEntry(t, entered, "a backlog request was not admitted while closed")
	}

	get(t, app, "/fail")
	require.Equal(t, circuitbreaker.StateOpen, cb.GetState())
	clock.Advance(time.Minute)

	// The whole backlog fails at once, after the deadline.
	releaseAll()
	for _, r := range stale {
		r.wait(t)
	}

	require.Equal(t, circuitbreaker.StateHalfOpen, cb.GetState(),
		"a backlog of stale failures must not keep pushing the recovery deadline out")
	require.Equal(t, http.StatusOK, get(t, app, "/ok").StatusCode,
		"a probe must still be admitted")
	require.Equal(t, circuitbreaker.StateClosed, cb.GetState())
}

// TestProbeFailingAfterSiblingClosedIsIgnored covers the state half of the
// matching rule, which the generation check alone does not: a probe of a
// half-open window can still be running when a sibling probe closes the
// circuit. Its failure belongs to a trial that is over, so it must not reopen
// a circuit that has just recovered.
func TestProbeFailingAfterSiblingClosedIsIgnored(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	cb := circuitbreaker.New(circuitbreaker.Config{
		FailureThreshold:      1,
		SuccessThreshold:      1,
		HalfOpenMaxConcurrent: 2, // room for the stranded probe and the one that closes
		Timeout:               time.Minute,
		Clock:                 clock.Now,
	})

	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	releaseStranded := releaser(t, release)

	app := gin.New()
	app.Use(circuitbreaker.Middleware(cb))
	app.GET("/slowfail", func(c *gin.Context) {
		entered <- struct{}{}
		<-release
		c.Status(http.StatusInternalServerError)
	})
	app.GET("/ok", func(c *gin.Context) {
		c.String(http.StatusOK, "OK")
	})
	app.GET("/fail", func(c *gin.Context) {
		c.Status(http.StatusInternalServerError)
	})

	trip(t, app, 1)
	clock.Advance(time.Minute)
	require.Equal(t, circuitbreaker.StateHalfOpen, cb.GetState())

	// A probe of this window that stays in flight and will fail.
	stranded := inBackground(app, "/slowfail")
	awaitEntry(t, entered, "the stranded probe was not admitted")

	// A sibling probe of the same window succeeds and closes the circuit.
	require.Equal(t, http.StatusOK, get(t, app, "/ok").StatusCode)
	require.Equal(t, circuitbreaker.StateClosed, cb.GetState())

	// The stranded probe now fails, against a circuit that has recovered.
	releaseStranded()
	stranded.wait(t)

	require.Equal(t, circuitbreaker.StateClosed, cb.GetState(),
		"a probe failing after its trial ended must not reopen the recovered circuit")

	// And a genuine failure still opens it, so the gate has not gone too far.
	get(t, app, "/fail")
	require.Equal(t, circuitbreaker.StateOpen, cb.GetState())
}
