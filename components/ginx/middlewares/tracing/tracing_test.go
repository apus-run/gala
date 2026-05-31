package tracing

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gavv/httpexpect/v2"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace/noop"
)

func GinHandler(r *gin.Engine) *gin.Engine {
	helloFun := func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "hello world",
		})
	}

	pingFun := func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "ping",
		})
	}

	fooFun := func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "foo",
		})
	}

	r.GET("/foo", fooFun)
	r.GET("/hello", helloFun)
	r.GET("/ping", pingFun)
	r.DELETE("/hello", helloFun)
	r.POST("/hello", helloFun)
	r.PUT("/hello", helloFun)
	r.PATCH("/hello", helloFun)

	return r
}

func TestTracing(t *testing.T) {
	previousLogger := slog.Default()
	t.Cleanup(func() {
		slog.SetDefault(previousLogger)
	})

	// Create a slog logger, which:
	//   - Logs to stdout.
	w := os.Stdout
	logger := slog.New(
		slog.NewJSONHandler(
			w,
			&slog.HandlerOptions{
				Level:     slog.LevelDebug,
				AddSource: true,
			},
		),
	)
	logger.WithGroup("http").
		With("environment", "production").
		With("server", "gin/1.9.0").
		With("server_start_time", time.Now()).
		With("gin_mode", gin.EnvGinMode)
	// [SetDefault]还更新了[log]包使用的默认logger
	slog.SetDefault(logger)

	gin.SetMode(gin.ReleaseMode)
	engine := gin.Default()

	engine.Use(Tracing("demo"))

	handler := GinHandler(engine)

	// run server using httptest
	server := httptest.NewServer(handler)
	defer server.Close()

	e := httpexpect.Default(t, server.URL)

	e.GET("/ping").
		Expect().
		Status(http.StatusOK).JSON().Object().HasValue("msg", "ping")
	e.GET("/foo").
		Expect().
		Status(http.StatusOK).JSON().Object().HasValue("msg", "foo")
	e.GET("/hello").
		Expect().
		Status(http.StatusOK).JSON().Object().HasValue("msg", "hello world")
}

type tracerProvider struct{}

func (t *tracerProvider) Inject(_ context.Context, _ propagation.TextMapCarrier) {
}

func (t *tracerProvider) Extract(ctx context.Context, _ propagation.TextMapCarrier) context.Context {
	return ctx
}

func (t *tracerProvider) Fields() []string {
	return []string{}
}

func TestWithPropagators(t *testing.T) {
	cfg := &traceConfig{}
	opt := WithPropagators(&tracerProvider{})
	opt(cfg)
}

func TestWithTracerProvider(t *testing.T) {
	cfg := &traceConfig{}
	opt := WithTracerProvider(noop.NewTracerProvider())
	opt(cfg)
}
