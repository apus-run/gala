// Command host demonstrates the plugin package end to end: a registry with
// explicit install ordering, four lifecycle phases, and a service shared
// between two plugins that never import each other.
//
// host 端到端演示 plugin 包：注册表、显式安装顺序、四个生命周期阶段，
// 以及两个互不 import 的插件之间共享服务。
//
// # Run | 运行
//
//	go run ./examples/host
package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/apus-run/gala/components/plugin"
)

// greeter is the shared service; the provider and the consumer only agree on
// this interface | 双方共享的服务接口，提供方与消费方只约定它。
type greeter interface {
	Greet(name string) string
}

type greeterImpl struct{ prefix string }

func (g greeterImpl) Greet(name string) string { return g.prefix + " " + name }

// App is the host: a lifecycle plus a service container.
// App 是宿主：一个生命周期加一个服务容器。
type App struct {
	Life     plugin.Lifecycle
	Services plugin.Container
}

// greetingPlugin provides the greeter; Select places it before its consumers.
// greetingPlugin 提供 greeter；应用方用 Select 把它排在消费者之前。
type greetingPlugin struct{ prefix string }

func (p *greetingPlugin) Name() string { return "greeting" }
func (p *greetingPlugin) Install(a *App) error {
	return plugin.Provide[greeter](&a.Services, greeterImpl{prefix: p.prefix})
}

// auditPlugin consumes the greeter (installed after its provider) and greets
// during startup; best-effort so telemetry never breaks the host.
// auditPlugin 在提供方之后安装，启动时取用服务问候；治理钩子用 BestEffort，
// 遥测失败不影响宿主。
type auditPlugin struct{ greeted *string }

func (p *auditPlugin) Name() string { return "audit" }
func (p *auditPlugin) Install(a *App) error {
	g, ok := plugin.Inject[greeter](&a.Services)
	if !ok {
		return errors.New("audit: no greeter provided")
	}
	a.Life.AfterStart(func(context.Context) error {
		*p.greeted = g.Greet("audit")
		return nil
	}, plugin.BestEffort())
	a.Life.AfterStop(func(context.Context) error {
		fmt.Println("audit: flushed")
		return nil
	}, plugin.BestEffort())
	return nil
}

// run mirrors the host pattern from the Lifecycle docs: AfterStop runs
// unconditionally, even when install or startup fails halfway.
// run 复刻 Lifecycle 文档里的宿主写法：AfterStop 无条件执行，
// 安装或启动中途失败也一样。
func run(ctx context.Context, reg *plugin.Registry[*App], a *App) (err error) {
	defer func() { err = errors.Join(err, a.Life.RunAfterStop(ctx)) }()
	ps, err := reg.Select("greeting", "audit")
	if err != nil {
		return err
	}
	if err := plugin.Install(a, ps...); err != nil {
		return err
	}
	if err := a.Life.RunBeforeStart(ctx); err != nil {
		return err
	}
	fmt.Println("-- serving --")
	if err := a.Life.RunAfterStart(ctx); err != nil {
		return err
	}
	return a.Life.RunBeforeStop(ctx)
}

func main() {
	greeted := ""
	reg := new(plugin.Registry[*App])
	reg.Register(&greetingPlugin{prefix: "hello"})
	reg.Register(&auditPlugin{greeted: &greeted})

	a := &App{}
	if err := run(context.Background(), reg, a); err != nil {
		fmt.Println("run failed:", err)
	}
	fmt.Println(greeted)
}
