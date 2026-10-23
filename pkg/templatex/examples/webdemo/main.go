// webdemo 展示 templatex 的推荐用法：
// 进程内共享一个 Registry、模板通过 embed.FS 编进二进制、
// layout+content 组合渲染，并在 handler 中统一处理解析/执行错误。
//
// 运行：go run ./examples/webdemo
// 然后访问 http://localhost:8080
package main

import (
	"embed"
	"log"
	"net/http"
	"strings"

	"github.com/apus-run/gala/pkg/templatex"
)

//go:embed templates
var templates embed.FS

// registry 进程内唯一，并发安全；重复 Load 同一组文件直接命中 LRU 缓存。
var registry = templatex.NewRegistry().AddFuncs(map[string]any{
	"upper": strings.ToUpper,
})

// PageData 模板入参用具体类型，字段拼错在编译期就能发现。
type PageData struct {
	Title string
	Name  string
}

// renderPage 统一走 layout + 页面内容组合；Render 的 err 同时覆盖解析错误与执行错误。
func renderPage(w http.ResponseWriter, page string, data PageData) {
	html, err := registry.LoadFS(
		templates,
		"templates/layout.html",
		"templates/pages/"+page,
	).Render(data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(html))
}

func main() {
	http.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		renderPage(w, "index.html", PageData{Title: "Index", Name: r.URL.Query().Get("name")})
	})
	http.HandleFunc("GET /about", func(w http.ResponseWriter, r *http.Request) {
		renderPage(w, "about.html", PageData{Title: "About"})
	})

	log.Println("listening on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
