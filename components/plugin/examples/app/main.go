// Command app demonstrates parts one and two combined — Registry + Install +
// Lifecycle, no Container: each plugin registers its own hooks inside
// Install, so hook order inherits the explicit install order, and stop hooks
// run in reverse. Service sharing is the third part's job (see
// examples/host).
//
// app 演示零件一与零件二的组合——Registry + Install + Lifecycle，不用
// Container：插件在自己的 Install 里挂钩子，钩子执行顺序继承安装顺序
// （由 Select 指定），停止阶段反序执行。服务共享是零件三的事（见 examples/host）。
//
// # Run | 运行
//
//	go run ./examples/app
package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/apus-run/gala/components/plugin"
)

// App carries only a Lifecycle; plugins arrive through the registry.
// App 只带一个 Lifecycle；插件从注册表来。
type App struct{ Life plugin.Lifecycle }

// servicePlugin stands for one layer of a service (config, db, http):
// install order encodes the dependency order — config first, http last.
// servicePlugin 代表服务的一层（config、db、http）：安装顺序即依赖顺序——
// config 最先，http 最后。
type servicePlugin struct {
	name string
}

func (p *servicePlugin) Name() string { return p.name }
func (p *servicePlugin) Install(a *App) error {
	fmt.Println("install:", p.name)
	// Start hooks run in install order: config is up before db connects,
	// db is up before http accepts traffic.
	// 启动钩子按安装顺序执行：db 连接前 config 已就绪，http 收流量前 db 已就绪。
	a.Life.BeforeStart(func(context.Context) error {
		fmt.Println("  start:", p.name)
		return nil
	})
	// Stop hooks run in reverse: http drains first, db closes only when
	// nothing reads it anymore, config goes last. That is the whole reason
	// stop order is reversed — like defer.
	// 停止钩子反序执行：http 先排空，没人在读之后 db 才关闭，config 最后收尾。
	// 停止反序的全部理由就在这——和 defer 一样。
	a.Life.BeforeStop(func(context.Context) error {
		fmt.Println("  stop: ", p.name)
		return nil
	})
	return nil
}

// run follows the host pattern from the Lifecycle docs: AfterStop joins
// unconditionally, even when startup fails halfway.
// run 复刻 Lifecycle 文档的宿主写法：AfterStop 无条件 Join，启动中途失败也一样。
func run(ctx context.Context, reg *plugin.Registry[*App]) (err error) {
	a := &App{}
	defer func() { err = errors.Join(err, a.Life.RunAfterStop(ctx)) }()

	ps, err := reg.Select("config", "db", "http")
	if err != nil {
		return err
	}
	if err = plugin.Install(a, ps...); err != nil {
		return err
	}
	if err = a.Life.RunBeforeStart(ctx); err != nil {
		return err
	}
	fmt.Println("-- serving --")
	if err = a.Life.RunAfterStart(ctx); err != nil {
		return err
	}
	err = a.Life.RunBeforeStop(ctx)
	return err
}

func main() {
	reg := new(plugin.Registry[*App])
	for _, p := range []*servicePlugin{
		{name: "http"},
		{name: "db"},
		{name: "config"},
	} {
		reg.Register(p)
	}

	if err := run(context.Background(), reg); err != nil {
		fmt.Println("run:", err)
	}
}
