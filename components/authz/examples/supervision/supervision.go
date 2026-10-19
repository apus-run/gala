// Package supervision demonstrates application-owned policy reload supervision.
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

	first := <-results // Observe either task's exit while the application is running.
	cancel()           // Stop the sibling task and wait for it exactly once.
	second := <-results
	var failures []error
	for _, err := range []error{first, second} {
		if err != nil && !errors.Is(err, context.Canceled) {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
