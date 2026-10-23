// ops 演示一个真实的定时任务值守服务：每分钟心跳、定时健康检查（含
// panic 恢复）、每日全量报表（防重叠）、固定时区的工作日晨报、非法表达
// 式的容错处理，以及收到信号后的优雅退出。
//
// 运行：go run ./examples/ops
// 观察每分钟的心跳输出，Ctrl-C 退出（daily-report 的 30 分钟模拟任务
// 演示 Shutdown 最多等 10 秒后带错误返回）。
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/apus-run/gala/components/cron"

	// 无系统 tz 数据库的环境（如 scratch 镜像）也能解析 CRON_TZ=Asia/Shanghai
	_ "time/tzdata"
)

func main() {
	c := cron.New(
		cron.WithPanicHandler(func(jobId string, r any, stack []byte) {
			// 真实场景：上报到监控；这里演示 job panic 不拖垮进程
			log.Printf("[panic] job %q: %v\n%s", jobId, r, stack)
		}),
	)

	// 1. 每分钟心跳（cron 只有分钟粒度，*/1 即每分钟）
	c.MustAdd("heartbeat", "* * * * *", func() {
		log.Println("[heartbeat] alive")
	})

	// 2. 每 5 分钟健康检查，第 3 次故意 panic，演示 PanicHandler 兜底
	checks := 0
	c.MustAdd("healthcheck", "*/5 * * * *", func() {
		checks++
		if checks == 3 {
			panic("db: connection refused")
		}
		log.Printf("[healthcheck] ok (%d)", checks)
	})

	// 3. 每日全量报表：任务很长，SkipIfRunning 防止上一次没跑完又开一次
	c.MustAdd("daily-report", "@daily", func() {
		log.Println("[daily-report] started")
		time.Sleep(30 * time.Minute) // 模拟长任务
		log.Println("[daily-report] done")
	}, cron.SkipIfRunning())

	// 4. 工作日早 9 点晨报：时区钉死在业务所在地，与部署机器无关
	c.MustAdd("morning-brief", "CRON_TZ=Asia/Shanghai 0 9 * * MON-FRI", func() {
		log.Println("[morning-brief] sent")
	})

	// 5. Add 返回 error：表达式非法时不会 panic（MustAdd 才会）
	if err := c.Add("typo", "61 * * * *", func() {}); err != nil {
		log.Printf("[typo] rejected: %v", err)
	}

	c.Start()

	log.Println("jobs scheduled:")
	for _, j := range c.Jobs() {
		log.Printf("  %-14s %-40s next: %s", j.ID, j.Expression, j.NextRun.Format("2006-01-02 15:04 -0700"))
	}

	// 等待退出信号，优雅停机
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	log.Println("shutting down...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v（仍有任务在后台运行）", err)
	}
	log.Println("bye")
}
