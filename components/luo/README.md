# luo

标准库 `http.ServeMux` 的路由分组封装：按路径前缀分组、挂载子组、给一组路由或单条路由叠加中间件。匹配仍由 `http.ServeMux` 完成，中间件组合复用 `pkg/chain`，组件不启动 goroutine。

## 安装

要求 **Go 1.27 或更高版本**。

```sh
go get github.com/apus-run/gala/components/luo
```

以下接口与示例对应当前仓库源码。使用尚未发布的变更时，在应用的 `go.mod` 中通过 `replace` 指向本地 `components/luo` 模块。

## 接口签名

```go
func New(mux *http.ServeMux) *Group
func Mount(mux *http.ServeMux, basePath string) *Group
func Wrap(handler http.Handler, mw func(http.Handler) http.Handler, more ...func(http.Handler) http.Handler) http.Handler

func (g *Group) Group() *Group
func (g *Group) Mount(basePath string) *Group
func (g *Group) With(mw func(http.Handler) http.Handler, more ...func(http.Handler) http.Handler) *Group
func (g *Group) Use(mw func(http.Handler) http.Handler, more ...func(http.Handler) http.Handler)

func (g *Group) Route(configureFn func(*Group))
func (g *Group) Handle(pattern string, handler http.Handler)
func (g *Group) HandleFunc(pattern string, handler http.HandlerFunc)
func (g *Group) Method(method, pattern string, handler http.HandlerFunc)
func (g *Group) GET(pattern string, handler http.HandlerFunc) // 同类入口：POST/PUT/PATCH/DELETE/HEAD/OPTIONS/CONNECT/TRACE/ANY
func (g *Group) HandleFiles(pattern string, root http.FileSystem)
func (g *Group) HandleRoot(method string, handler http.Handler)
func (g *Group) HandleRootFunc(method string, handler http.HandlerFunc)
func (g *Group) NotFoundHandler(handler http.HandlerFunc)
func (g *Group) Handler(r *http.Request) (http.Handler, string)
func (g *Group) ServeHTTP(w http.ResponseWriter, r *http.Request)

type HandlerFunc func(http.ResponseWriter, *http.Request) error
type ErrorHandler func(http.ResponseWriter, *http.Request, error)
func Adapt(handler HandlerFunc, onError ErrorHandler) http.HandlerFunc
```

| 构造入口 | 用途 |
| --- | --- |
| `New(mux)` | 根组，无路径前缀 |
| `Mount(mux, base)` | 根组 + 路径前缀 |
| `Group()` | 派生同级子组，继承当前中间件 |
| `Mount(base)` | 派生子组并追加路径前缀 |
| `With(mw...)` | 派生带额外中间件的子组，不改动原组 |

根组与子组是同一个类型，都实现 `http.Handler`，可直接传给 `http.ListenAndServe`。

## 快速开始

路由模式沿用标准库语法（`"GET /users/{id}"`、`"/static/"`）。下面的程序可直接运行：

```go
package main

import (
    "fmt"
    "net/http"
    "net/http/httptest"

    "github.com/apus-run/gala/components/luo"
)

func main() {
    mux := http.NewServeMux()
    router := luo.New(mux)

    // 根组中间件：对所有请求生效，此时只知道匹配到的模式
    router.Use(func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            w.Header().Set("X-Pattern", r.Pattern)
            next.ServeHTTP(w, r)
        })
    })

    router.HandleFunc("GET /hello", func(w http.ResponseWriter, r *http.Request) {
        fmt.Fprint(w, "hello")
    })

    // 子组：/api 前缀，中间件在路由之后执行，能读到路径参数
    api := router.Mount("/api")
    api.Use(func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            fmt.Fprintf(w, "id=%s ", r.PathValue("id"))
            next.ServeHTTP(w, r)
        })
    })
    api.HandleFunc("GET /users/{id}", func(w http.ResponseWriter, r *http.Request) {
        fmt.Fprint(w, "user")
    })

    for _, path := range []string{"/hello", "/api/users/7"} {
        rec := httptest.NewRecorder()
        router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
        fmt.Printf("%s -> %d %q X-Pattern=%q\n", path, rec.Code, rec.Body.String(), rec.Header().Get("X-Pattern"))
    }
}
```

输出：

```text
/hello -> 200 "hello" X-Pattern="GET /hello"
/api/users/7 -> 200 "id=7 user" X-Pattern="GET /api/users/{id}"
```

生产用法是 `http.ListenAndServe(":8080", router)`：注册完路由后把组本身当 handler 传进去，它负责套上根组中间件并交给底层 mux。

## 方法快捷入口与错误处理

`GET`、`POST`、`PUT`、`PATCH`、`DELETE`、`HEAD`、`OPTIONS`、`CONNECT`、`TRACE` 均通过现有 `HandleFunc` 注册，保留分组前缀、中间件及根路径精确匹配语义：

```go
api.GET("/users/{id}", getUser)
api.Method("PROPFIND", "/files/{path...}", listFiles)
api.ANY("/fallback", fallback)
```

上述 handler 都使用标准 `http.HandlerFunc` 签名。`ANY` 注册不带方法约束的模式，支持自定义 HTTP 方法；同路径上更具体的方法路由优先。`GET` 同时匹配 `HEAD`，显式 `HEAD` 路由优先。`Method` 的空方法与 `ANY` 等价，不使用 `"*"` 代表任意方法。

业务函数如果需要返回 `error`，通过 `Adapt` 转换即可，无需改变路由或中间件接口：

```go
// loadUser 是业务函数，类型为 luo.HandlerFunc。
api.GET("/users/{id}", luo.Adapt(loadUser,
    func(w http.ResponseWriter, r *http.Request, err error) {
        // 在这里记录错误，并按业务约定选择状态码及 JSON/HTML 等响应格式。
        http.Error(w, "request failed", http.StatusInternalServerError)
    },
))
```

`Adapt` 的两个参数必须非 nil，配置错误会在构造时 panic。业务 handler 返回 nil 时不调用错误处理器；返回非 nil 错误时调用一次。适配器保留原始 `ResponseWriter`，不限制 SSE、Flush 或 Hijack。业务 handler 应在写响应前返回错误；如果已经写出响应，错误处理器应只记录错误，避免再次写入。组件不预设错误类型、状态码映射或编解码格式。

## 中间件

| 添加位置 | 生效范围 | 执行时机 |
| --- | --- | --- |
| 根组 `Use(...)` | 所有请求 | 路由匹配前，`r.Pattern` 有值，`r.PathValue()` 为空 |
| 子组 `Use(...)` | 该组及其后代注册的路由 | 路由匹配后，可读 `r.PathValue()` |
| `With(...)` 后接的调用 | 只作用于紧接着注册的那条路由 | 同上 |

- 中间件按添加顺序执行。
- 同一个组注册过路由之后再调用 `Use` 会 panic（`luo: Use called after routes were registered on this group`）。需要在注册之后追加中间件时，用 `With` 或 `Group` 派生新组——派生出的组不携带"已注册路由"状态，可以继续 `Use`。
- `Route` 在根组上调用时会自动派生一个子组，路由和中间件写在回调里，不会污染根组的中间件栈。
- 路由注册只应在启动阶段、单个 goroutine 中完成。组件不保证并发注册安全，只保证注册完成后并发读取（即正常处理请求）是安全的。

## 根路径与 404

- 根路径 `"/"` 有两种语义：`HandleFunc("/")` 会注册成 `"/{$}"`，只匹配该组的根路径；`Handle("/")` 保留结尾斜杠模式，注册成 `"/"`，会兜住该组下所有未被更具体模式匹配的路径（catch-all）。要"只处理根路径"用前者。
- 挂载组的根路径（如 `/api`）用 `HandleRoot`/`HandleRootFunc` 注册：`Handle("/")` 这类结尾斜杠模式会让 `http.ServeMux` 对 `/api` 回 301 跳到 `/api/`，`HandleRoot` 直接注册 `/api` 返回 200，省掉这次往返。`method` 传空字符串表示匹配所有方法。
- 方法不匹配返回 `405 Method Not Allowed` 并带 `Allow` 头，路径清理重定向由 `http.ServeMux` 处理，这两类响应都不经过自定义 404。
- `NotFoundHandler(fn)` 只在该组确实无路由匹配时触发，且总是设置在根组上（在子组上调用也会写到根组）。`DisableNotFoundHandler()` 已废弃，无效果。

## 静态文件与单条路由

```go
// 等价于 http.FileServer + http.StripPrefix，并套上该组的中间件
router.HandleFiles("/static/", http.Dir("assets/static"))

// 不建组，只给一条路由套中间件
mux.Handle("/hello", luo.Wrap(helloHandler, loggingMiddleware, corsMiddleware))
```

`Wrap` 至少需要一个中间件：第一个是必填参数，其余为可变参数。

---

改写自 [go-pkgz/routegroup](https://github.com/go-pkgz/routegroup)（MIT）。
