// Package supervision demonstrates application-owned policy reload supervision | 演示应用自持的策略重载监管。
package supervision

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/apus-run/gala/components/authz"
	redisadapter "github.com/apus-run/gala/components/authz/adapter/redis"
	"github.com/redis/go-redis/v9"
)

// Run serves until cancellation or either task exits. serve must stop accepting
// work on cancellation and drain requests, policy writes and SyncPolicy calls
// before returning. The caller closes client only after Run returns.
// Run is an example for one policy namespace; use your own key and channel.
// Run 一直服务到被取消或任一任务退出。取消时 serve 必须停止接收新请求，
// 并在返回前排空请求、策略写入与 SyncPolicy 调用。
// 调用方只在 Run 返回之后才关闭 client。
// Run 是单个策略命名空间的示例；请换成你自己的键与频道。
func Run(ctx context.Context, client interface {
	redis.Cmdable
	redisadapter.WatcherClient
}, modelPath string,
	serve func(context.Context, *authz.Authorizer) error,
) error {
	watcher, err := redisadapter.NewWatcher(ctx, client, "authz:tenant-a:changed")
	if err != nil {
		return err
	}
	defer watcher.Close()
	adapter, err := redisadapter.NewAdapter(&redisadapter.Config{
		Client: client, Key: "authz:tenant-a:rules",
	})
	if err != nil {
		return err
	}
	az, err := authz.New(
		authz.WithModelFilePath(modelPath),
		authz.WithPolicyAdapter(adapter),
		authz.WithWatcher(watcher),
		authz.WithWatcherErrorHandler(func(err error) { log.Print(err) }),
		authz.WithMaxPolicyAge(30*time.Second),
	)
	if err != nil {
		return err
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan error, 2)
	go func() { results <- az.RunPolicyReload(runCtx, 5*time.Second, func(err error) { log.Print(err) }) }()
	go func() { results <- serve(runCtx, az) }()

	// Observe either task's exit while the application is running.
	// 在应用运行期间观察任一任务的退出。
	first := <-results
	// Stop the sibling task and wait for it exactly once.
	// 停止兄弟任务，并且恰好等待它一次。
	cancel()
	second := <-results
	var failures []error
	for _, err := range []error{first, second} {
		if err != nil && !errors.Is(err, context.Canceled) {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
