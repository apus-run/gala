// Command registry demonstrates part one of the plugin package on its own:
// Plugin / Registry / Install. The scenario is an HTTP app: the host is a
// plain struct — NOT a gala server — and the MVC layer wrapped by ServeMux
// or Gin is just another Plugin[H]. The plugin package itself imports
// neither net/http nor any framework; H is entirely up to the caller.
//
// registry 单独演示 plugin 的零件一：Plugin / Registry / Install。场景是一个
// HTTP 应用：宿主是普通 struct——不是 gala server——ServeMux 或 Gin 封装的
// MVC 服务在这里只是一种 Plugin[H]。plugin 包自身不 import net/http、不绑定
// 任何框架，宿主类型 H 完全由使用方决定。
//
// # Run | 运行
//
//	go run ./examples/registry
package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/apus-run/gala/components/plugin"
)

// App is the host: just a struct with a mux. Swap *http.ServeMux for a
// *gin.Engine and the plugins below stay the same shape.
// App 是宿主：一个带 mux 的普通 struct。把 *http.ServeMux 换成
// *gin.Engine，插件的形状不变（见 examples/ginmvc）。
type App struct {
	Mux *http.ServeMux
}

// healthPlugin is infrastructure: installed first through Select.
// healthPlugin 是基础设施：通过 Select 最先安装。
type healthPlugin struct{}

func (healthPlugin) Name() string { return "health" }
func (healthPlugin) Install(a *App) error {
	a.Mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "ok")
	})
	return nil
}

// userPlugin is the MVC service: the "controller" is just an Install that
// wires routes onto the host.
// userPlugin 就是 MVC 服务："controller" 不过是一个往宿主挂路由的 Install。
type userPlugin struct{ users []string }

func (*userPlugin) Name() string { return "users" }

func (p *userPlugin) Install(a *App) error {
	a.Mux.HandleFunc("GET /users", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, "%v", p.users)
	})
	a.Mux.HandleFunc("POST /users", func(w http.ResponseWriter, r *http.Request) {
		p.users = append(p.users, r.URL.Query().Get("name"))
		w.WriteHeader(http.StatusCreated)
	})
	return nil
}

func main() {
	reg := new(plugin.Registry[*App])
	reg.Register(&healthPlugin{})
	reg.Register(&userPlugin{users: []string{"ada", "linus"}})

	// Install wires everything in the order selected by the application onto a
	// host — no gala server, no framework bootstrap.
	// Install 按应用方选择的顺序把一切装配到一个随手构造的宿主上——
	// 没有 gala server，没有框架引导。
	app := &App{Mux: http.NewServeMux()}
	ps, err := reg.Select("health", "users")
	if err != nil {
		fmt.Println("select:", err)
		return
	}
	if err := plugin.Install(app, ps...); err != nil {
		fmt.Println("install:", err)
		return
	}

	// The assembled mux already serves; probe it in-process.
	// 装配完成的 mux 已经可以服务，进程内直接探测。
	for _, probe := range []struct{ method, path string }{
		{"GET", "/healthz"},
		{"GET", "/users"},
		{"POST", "/users?name=grace"},
		{"GET", "/users"},
	} {
		req := httptest.NewRequest(probe.method, probe.path, nil)
		rec := httptest.NewRecorder()
		app.Mux.ServeHTTP(rec, req)
		fmt.Printf("%-6s %-20s -> %d %s\n", probe.method, probe.path, rec.Code, rec.Body.String())
	}
}
