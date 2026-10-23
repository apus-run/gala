// publish 演示 hook 在真实业务里的用法：一条“文章发布”事件驱动的处理链。
//
// 校验、敏感词过滤、核心发布、审计日志按 Priority 排队执行；
// 校验失败不调 Next() 直接拦下整条链（停止是 Next 形状的，不是 error 形状的）；
// TaggedHook 给带 vip 标签的文章追加加积分 handler；
// Trigger 的临时 handler 做一次性收尾——链被拦截时它自然不会执行。
//
// 运行：go run ./examples/publish
package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/apus-run/gala/components/hook"
)

// ArticleEvent 文章发布事件：嵌入 hook.Event 获得 Next 链控制权
type ArticleEvent struct {
	hook.Event

	Title  string
	Author string
	IsVIP  bool
	Status string // pending -> published / rejected
	Reason string
}

// Tags 实现 hook.Tagger：事件的标签决定哪些 TaggedHook 的 handler 会触发
func (e *ArticleEvent) Tags() []string {
	if e.IsVIP {
		return []string{"vip"}
	}
	return nil
}

func main() {
	publish := &hook.Hook[*ArticleEvent]{}

	auditBroken := false // 最后一个场景切到 true，演示 error 上浮

	// Priority 越小越先执行：校验最先，审计最后
	publish.Bind(&hook.Handler[*ArticleEvent]{
		Id:       "validate",
		Priority: -20,
		Func: func(e *ArticleEvent) error {
			if strings.TrimSpace(e.Title) == "" {
				e.Status, e.Reason = "rejected", "标题不能为空"
				return nil // 不调 Next：拦截整条链
			}
			fmt.Printf("[validate] ok: %q\n", e.Title)
			return e.Next()
		},
	})

	publish.Bind(&hook.Handler[*ArticleEvent]{
		Id:       "wordfilter",
		Priority: -10,
		Func: func(e *ArticleEvent) error {
			if strings.Contains(e.Title, "广告") {
				e.Status, e.Reason = "rejected", "命中敏感词"
				return nil
			}
			fmt.Println("[wordfilter] clean")
			return e.Next()
		},
	})

	// 核心发布（默认 Priority 0，同优先级按注册顺序）
	publish.BindFunc(func(e *ArticleEvent) error {
		e.Status = "published"
		fmt.Printf("[publish] %q is now public\n", e.Title)
		return e.Next()
	})

	// VIP 专属 handler：挂在同一个 hook 上，但只对带 vip 标签的事件生效
	vip := hook.NewTaggedHook(publish, "vip")
	vip.BindFunc(func(e *ArticleEvent) error {
		fmt.Println("[vip-bonus] +50 积分")
		return e.Next()
	})

	publish.Bind(&hook.Handler[*ArticleEvent]{
		Id:       "audit",
		Priority: 10,
		Func: func(e *ArticleEvent) error {
			fmt.Printf("[audit] %s -> %s\n", e.Author, e.Status)
			if auditBroken {
				return errors.New("审计写入失败") // error 只上浮，已发生的处理不会回滚
			}
			return e.Next()
		},
	})

	scenarios := []struct {
		name        string
		event       *ArticleEvent
		breakAudit  bool
	}{
		{"VIP 正常发布", &ArticleEvent{Title: "Go 泛型实战", Author: "ada", IsVIP: true}, false},
		{"普通作者正常发布", &ArticleEvent{Title: "Hello Hook", Author: "bob"}, false},
		{"敏感词被拦截", &ArticleEvent{Title: "低息贷款广告", Author: "spam"}, false},
		{"审计故障：error 上浮且后续 handler 被跳过", &ArticleEvent{Title: "Try Again", Author: "ada"}, true},
	}

	for _, s := range scenarios {
		fmt.Printf("\n=== %s ===\n", s.name)
		auditBroken = s.breakAudit

		err := publish.Trigger(s.event,
			// 一次性 handler：仅本次触发生效；链在前面被拦截/中断时不会执行
			func(e *ArticleEvent) error {
				fmt.Printf("[notify] %q 最终状态: %s\n", e.Title, e.Status)
				return nil
			},
		)

		fmt.Printf("[result] status=%s err=%v\n", s.event.Status, err)
	}
}
