// email 演示 hook 的“观察者”用法，与 publish 的“拦截器”用法互补：
// 用户注册事件广播给若干互相独立的监听器——欢迎邮件、营销订阅、CRM 同步、
// Pro 专属跟进。
//
// 观察者模式的四条纪律都在这里：
//   - 监听器只观察、不拦截：处理完必须调 e.Next()
//   - 单个监听器失败自己扛：记日志后仍调 Next()，把 error 返回给 Trigger 上报
//   - 监听器可运行时摘除（Unbind）——营销开关
//   - 监听器可热替换（Bind 同 Id）——欢迎邮件模板更新，不重启进程
//
// 运行：go run ./examples/email
package main

import (
	"errors"
	"fmt"

	"github.com/apus-run/gala/components/hook"
)

// UserEvent 用户注册事件
type UserEvent struct {
	hook.Event

	Name    string
	Email   string
	Plan    string // free / pro
	CRMDown bool   // 模拟 CRM 系统抖动
}

// Tags 实现 hook.Tagger：Pro 用户带 pro 标签，触发专属监听器
func (e *UserEvent) Tags() []string {
	if e.Plan == "pro" {
		return []string{"pro"}
	}
	return nil
}

func main() {
	onCreated := &hook.Hook[*UserEvent]{}

	// 1. 欢迎邮件：固定 Id，模板更新时用同 Id 热替换
	onCreated.Bind(&hook.Handler[*UserEvent]{
		Id: "welcome",
		Func: func(e *UserEvent) error {
			fmt.Printf("[welcome] 模板v1 -> %s\n", e.Email)
			return e.Next()
		},
	})

	// 2. 营销订阅：记下 Id，运营开关关闭时随时 Unbind
	newsletter := onCreated.BindFunc(func(e *UserEvent) error {
		fmt.Printf("[newsletter] %s 已订阅产品周报\n", e.Email)
		return e.Next()
	})

	// 3. CRM 同步：自己的错误自己扛，绝不拦事件
	onCreated.BindFunc(func(e *UserEvent) error {
		if e.CRMDown {
			fmt.Println("[crm] 同步超时，已转入重试队列；继续传播事件")
			err := errors.New("crm: sync timeout")
			_ = e.Next() // 即使失败也让后面的监听器照常执行
			return err   // 自己的失败上报给 Trigger 调用方
		}
		fmt.Printf("[crm] 同步完成：%s\n", e.Name)
		return e.Next()
	})

	// 4. Pro 专属跟进：挂在同一个 hook 上，只对带 pro 标签的事件生效
	pro := hook.NewTaggedHook(onCreated, "pro")
	pro.BindFunc(func(e *UserEvent) error {
		fmt.Printf("[pro] %s 的专属客户经理已就位\n", e.Email)
		return e.Next()
	})

	fire := func(u *UserEvent) {
		fmt.Printf("[trigger] %s 注册，err=%v\n", u.Name, onCreated.Trigger(u))
	}

	fmt.Printf("listeners: %d\n", onCreated.Length())

	fmt.Println("\n=== 免费用户注册 ===")
	fire(&UserEvent{Name: "alice", Email: "alice@example.com", Plan: "free"})

	fmt.Println("\n=== Pro 用户注册：TaggedHook 追加专属跟进 ===")
	fire(&UserEvent{Name: "bob", Email: "bob@example.com", Plan: "pro"})

	fmt.Println("\n=== 运营关闭营销开关：Unbind 摘除订阅监听器 ===")
	onCreated.Unbind(newsletter)
	fmt.Printf("listeners: %d\n", onCreated.Length())
	fire(&UserEvent{Name: "carol", Email: "carol@example.com", Plan: "free"})

	fmt.Println("\n=== 欢迎邮件模板热更新：同 Id 重新 Bind ===")
	onCreated.Bind(&hook.Handler[*UserEvent]{
		Id: "welcome", // 同 Id = 原地替换，Length 不变
		Func: func(e *UserEvent) error {
			fmt.Printf("[welcome] 模板v2（新排版）-> %s\n", e.Email)
			return e.Next()
		},
	})
	fmt.Printf("listeners: %d（替换不新增）\n", onCreated.Length())
	fire(&UserEvent{Name: "dave", Email: "dave@example.com", Plan: "free"})

	fmt.Println("\n=== CRM 抖动：所有监听器照常执行，error 原样上浮 ===")
	fire(&UserEvent{Name: "eve", Email: "eve@example.com", Plan: "pro", CRMDown: true})
}
