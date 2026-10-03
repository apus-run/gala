package circuitbreaker_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/apus-run/gala/components/ginx/middlewares/circuitbreaker"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGinErrorsReachFailureDetector(t *testing.T) {
	failure := errors.New("downstream unavailable")
	cb := circuitbreaker.New(circuitbreaker.Config{
		FailureThreshold: 1,
		IsFailure:        func(c *gin.Context, err error) bool { return errors.Is(err, failure) },
	})
	router := gin.New()
	router.Use(cb.Build())
	router.GET("/", func(c *gin.Context) { _ = c.Error(failure); c.Status(200) })
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/", nil))
	require.Equal(t, 200, response.Code)
	require.Equal(t, circuitbreaker.StateOpen, cb.GetState())
}

func TestGinDefaultFailureDetectorAndUnrelatedErrors(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		before, during, clear bool
		state                 circuitbreaker.State
	}{
		{name: "new error", during: true, state: circuitbreaker.StateOpen},
		{name: "earlier middleware error", before: true, state: circuitbreaker.StateClosed},
		{name: "error list cleared", before: true, clear: true, state: circuitbreaker.StateClosed},
		{name: "client error", state: circuitbreaker.StateClosed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cb := circuitbreaker.New(circuitbreaker.Config{FailureThreshold: 1})
			router := gin.New()
			router.Use(func(c *gin.Context) {
				if tc.before {
					_ = c.Error(errors.New("earlier error"))
				}
				c.Next()
			}, cb.Build())
			router.GET("/", func(c *gin.Context) {
				if tc.during {
					_ = c.Error(errors.New("downstream error"))
				}
				if tc.clear {
					c.Errors = nil
				}
				c.Status(400)
			})
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest("GET", "/", nil))
			require.Equal(t, 400, response.Code)
			require.Equal(t, tc.state, cb.GetState())
		})
	}
}

func TestGinPanicCountsRegardlessOfRecoveryOrder(t *testing.T) {
	for _, outer := range []bool{true, false} {
		t.Run(map[bool]string{true: "outer recovery", false: "inner recovery"}[outer], func(t *testing.T) {
			clock := newFakeClock()
			cb := circuitbreaker.New(circuitbreaker.Config{FailureThreshold: 1, Timeout: time.Minute, Clock: clock.Now})
			router := gin.New()
			recoverHandler := gin.RecoveryWithWriter(io.Discard)
			if outer {
				router.Use(recoverHandler, cb.Build())
			} else {
				router.Use(cb.Build(), recoverHandler)
			}
			router.GET("/panic", func(*gin.Context) { panic("failed") })
			router.GET("/ok", func(c *gin.Context) { c.Status(204) })
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest("GET", "/panic", nil))
			require.Equal(t, 500, response.Code)
			require.Equal(t, circuitbreaker.StateOpen, cb.GetState())
			clock.Advance(time.Minute)
			response = httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest("GET", "/panic", nil))
			require.Equal(t, 500, response.Code)
			require.Equal(t, circuitbreaker.StateOpen, cb.GetState())
			clock.Advance(time.Minute)
			response = httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest("GET", "/ok", nil))
			require.Equal(t, 204, response.Code)
			require.Equal(t, circuitbreaker.StateClosed, cb.GetState(), "panic must release the half-open slot")
		})
	}
}

func TestGinRejectsBeforeDownstreamEvenWithCustomResponse(t *testing.T) {
	cb := circuitbreaker.New(circuitbreaker.Config{OnOpen: func(c *gin.Context) { c.Status(http.StatusServiceUnavailable) }})
	cb.ForceOpen()
	router := gin.New()
	router.Use(cb.Build())
	router.GET("/", func(*gin.Context) { t.Fatal("refused request reached business handler") })
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/", nil))
	require.Equal(t, 503, response.Code)
	require.Panics(t, func() { circuitbreaker.Middleware(nil) })
}
