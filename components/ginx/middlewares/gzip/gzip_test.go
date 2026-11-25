package gzip

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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
	gin.SetMode(gin.ReleaseMode)
	engine := gin.Default()

	engine.Use()

	handler := GinHandler(engine)

	engine.NoRoute(NewBuilder().
		SetLevel(DefaultCompression).
		Build(WithGzipExcludedExtensions([]string{"", ".html"})), func(c *gin.Context) {
		c.Header("Cache-Control", fmt.Sprintf("public, max-age=%d, public", 31536000))
		c.Header("Expires", time.Now().AddDate(1, 0, 0).Format("Mon, 01 Jan 2006 00:00:00 GMT"))

		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "404",
		})
	})

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
