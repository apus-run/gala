package accesslog

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
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

func TestRequest(t *testing.T) {
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

	// A local stand-in for the requstid middleware keeps this module free of a
	// dependency on a sibling module just for a test fixture.
	// 用本地替身取代 requstid 中间件，避免仅为测试依赖一个同级的子模块。
	engine.Use(func(c *gin.Context) {
		c.Header("X-Request-ID", "test-request")
		c.Next()
	})
	// custom print log
	engine.Use(NewBuilder(
		func(ctx context.Context, al *AccessLog) {
			logger.Debug("Gin 收到请求", slog.Any("req", al))
		}).
		AllowReqBody().
		AllowRespBody().
		MaxLength(1024).
		IgnoreRoutes("/foo").
		Build())

	handler := GinHandler(engine)

	// run server using httptest
	server := httptest.NewServer(handler)
	defer server.Close()

	for _, tc := range []struct{ path, want string }{
		{"/ping", "ping"},
		{"/foo", "foo"},
		{"/hello", "hello world"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			resp, err := http.Get(server.URL + tc.path)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("want 200, got %d", resp.StatusCode)
			}
			var body map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["msg"] != tc.want {
				t.Fatalf("want msg %q, got %v", tc.want, body["msg"])
			}
		})
	}
}

func TestPortlessHostDoesNotPanic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var got *AccessLog
	r := gin.New()
	r.Use(NewBuilder(func(_ context.Context, al *AccessLog) { got = al }).Build())
	r.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })

	// Host without an explicit port used to panic the logger (index out of range).
	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if got == nil || got.IP != "example.com" || got.Port != "" {
		t.Fatalf("unexpected AccessLog: %+v", got)
	}
}
