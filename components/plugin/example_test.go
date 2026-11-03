package plugin_test

import (
	"context"
	"errors"
	"fmt"

	"github.com/apus-run/gala/components/plugin"
)

type app struct{ Life plugin.Lifecycle }

// govPlugin 用一个函数描述"这个插件在哪些环节挂什么钩子"。
type govPlugin struct {
	name  string
	setup func(*app)
}

func (p govPlugin) Name() string         { return p.name }
func (p govPlugin) Install(a *app) error { p.setup(a); return nil }

// 治理型插件（日志、链路、监控）由应用方用 Select 排在业务插件之前，顺序一目了然，不需要任何数字：
// 启动时最先初始化；停止时因为反序，最后冲刷，
// 所以链路冲刷时产生的日志，还来得及被日志插件写出去。
func Example_governance() {
	say := func(s string) plugin.Hook {
		return func(context.Context) error { fmt.Println(s); return nil }
	}

	reg := new(plugin.Registry[*app])
	reg.Register(govPlugin{"logging", func(a *app) {
		a.Life.BeforeStart(say("logging: init"), plugin.BestEffort())
		a.Life.AfterStop(say("logging: flush"), plugin.BestEffort())
	}})
	reg.Register(govPlugin{"tracing", func(a *app) {
		a.Life.BeforeStart(say("tracing: init"), plugin.BestEffort())
		a.Life.AfterStop(say("tracing: flush"), plugin.BestEffort())
	}})
	reg.Register(govPlugin{"metrics", func(a *app) {
		a.Life.BeforeStart(say("metrics: register"), plugin.BestEffort())
		a.Life.AfterStart(say("metrics: ready"), plugin.BestEffort())
		a.Life.BeforeStop(say("metrics: draining"), plugin.BestEffort())
	}})
	reg.Register(govPlugin{"db", func(a *app) {
		a.Life.AfterStart(say("db: warmup"))
		a.Life.BeforeStop(say("db: drain"))
	}})

	a, ctx := &app{}, context.Background()
	ps, _ := reg.Select("logging", "tracing", "metrics", "db")
	_ = plugin.Install(a, ps...)

	_ = a.Life.RunBeforeStart(ctx)
	fmt.Println("-- servers running --")
	_ = a.Life.RunAfterStart(ctx)

	_ = a.Life.RunBeforeStop(ctx)
	fmt.Println("-- servers stopped --")
	_ = a.Life.RunAfterStop(ctx)

	// Output:
	// logging: init
	// tracing: init
	// metrics: register
	// -- servers running --
	// metrics: ready
	// db: warmup
	// db: drain
	// metrics: draining
	// -- servers stopped --
	// tracing: flush
	// logging: flush
}

// 启动中途失败：AfterStop 仍然执行，日志和链路照常冲刷，失败原因不会丢。
var errConnect = errors.New("db: connect failed")

func Example_startupFailure() {
	say := func(s string) plugin.Hook {
		return func(context.Context) error { fmt.Println(s); return nil }
	}

	reg := new(plugin.Registry[*app])
	reg.Register(govPlugin{"logging", func(a *app) {
		a.Life.BeforeStart(say("logging: init"), plugin.BestEffort())
		a.Life.AfterStop(say("logging: flush"), plugin.BestEffort())
	}})
	reg.Register(govPlugin{"tracing", func(a *app) {
		a.Life.BeforeStart(say("tracing: init"), plugin.BestEffort())
		a.Life.AfterStop(say("tracing: flush"), plugin.BestEffort())
	}})
	reg.Register(govPlugin{"db", func(a *app) {
		a.Life.BeforeStart(func(context.Context) error { return errConnect })
	}})

	a, ctx := &app{}, context.Background()
	ps, _ := reg.Select("logging", "tracing", "db")
	_ = plugin.Install(a, ps...)

	err := func() (err error) {
		defer func() { err = errors.Join(err, a.Life.RunAfterStop(ctx)) }()
		return a.Life.RunBeforeStart(ctx)
	}()
	fmt.Println("startup failed:", errors.Is(err, errConnect))

	// Output:
	// logging: init
	// tracing: init
	// tracing: flush
	// logging: flush
	// startup failed: true
}

// ---- 跨插件调用：Install 只登记，调用放到 AfterStart ----

type api struct {
	Life   plugin.Lifecycle
	routes map[string]func() string
}

type routePlugin struct{ name, path, call string }

func (p routePlugin) Name() string { return p.name }

func (p routePlugin) Install(a *api) error {
	if a.routes == nil {
		a.routes = map[string]func() string{}
	}
	a.routes[p.path] = func() string { return p.name + " handled " + p.path }

	if p.call != "" {
		// 不在 Install 里调用别的插件的接口：对方可能还没装。
		// 放到 AfterStart：此时所有插件都已安装完，调用与安装顺序无关。
		a.Life.AfterStart(func(context.Context) error {
			h, ok := a.routes[p.call]
			if !ok {
				return fmt.Errorf("no route %s", p.call)
			}
			fmt.Println(p.name, "->", h())
			return nil
		})
	}
	return nil
}

// orders 先于 users 安装，却要调用 users 的接口。
func Example_crossPluginCall() {
	a, ctx := &api{}, context.Background()

	_ = plugin.Install(a,
		routePlugin{name: "orders", path: "/orders", call: "/users"},
		routePlugin{name: "users", path: "/users"},
	)
	_ = a.Life.RunAfterStart(ctx)

	// Output:
	// orders -> users handled /users
}
