// Command lifecycle demonstrates part two of the plugin package on its own:
// Lifecycle — no Registry, no Container. The zero value is usable and hooks
// are attached directly by the host.
//
// lifecycle 单独演示 plugin 的零件二：Lifecycle——不用 Registry，也不用
// Container。零值可用，钩子由宿主直接挂。
//
// # Run | 运行
//
//	go run ./examples/lifecycle
package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/apus-run/gala/components/plugin"
)

// App carries only a Lifecycle: the host decides when phases run.
// App 只有一个 Lifecycle：何时执行各阶段由宿主决定。
type App struct{ Life plugin.Lifecycle }

func run(ctx context.Context, a *App) (err error) {
	// AfterStop runs unconditionally — even if startup fails halfway, the
	// flush below still happens.
	// AfterStop 无条件执行：即使启动中途失败，下面的冲刷也会发生。
	defer func() { err = errors.Join(err, a.Life.RunAfterStop(ctx)) }()

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
	a := &App{}

	// Critical hooks: config must load or the app refuses to start; the
	// connection pool must drain on stop.
	// 必须成功的钩子：配置加载失败就不启动；连接池停止前必须排空。
	a.Life.BeforeStart(func(context.Context) error {
		fmt.Println("load config")
		return nil
	})
	a.Life.BeforeStop(func(context.Context) error {
		fmt.Println("drain connection pool")
		return nil
	})
	a.Life.AfterStop(func(context.Context) error {
		fmt.Println("flush logs")
		return nil
	})

	// Governance hooks are best-effort: a metrics failure must not take the
	// app down with it.
	// 治理钩子尽力而为：指标预热失败不能拖垮应用。
	a.Life.AfterStart(func(context.Context) error {
		return errors.New("metrics endpoint unreachable")
	}, plugin.BestEffort())

	if err := run(context.Background(), a); err != nil {
		fmt.Println("run:", err)
	}
}
