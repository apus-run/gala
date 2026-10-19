package supervision

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/apus-run/gala/components/authz"
	"github.com/redis/go-redis/v9"
)

func TestRunSupervisesServiceExitAndCancellation(t *testing.T) {
	for _, cancelRun := range []bool{false, true} {
		t.Run(map[bool]string{false: "service_error", true: "parent_cancel"}[cancelRun], func(t *testing.T) {
			server := miniredis.RunT(t)
			client := redis.NewClient(&redis.Options{Addr: server.Addr()})
			defer client.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			failure := errors.New("service stopped")
			entered, done := make(chan struct{}), make(chan error, 1)
			go func() {
				done <- Run(ctx, client, "../model.conf", func(ctx context.Context, _ *authz.Authorizer) error {
					close(entered)
					if cancelRun {
						<-ctx.Done()
						return ctx.Err()
					}
					return failure
				})
			}()
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("service did not start")
			}
			if cancelRun {
				cancel()
			}
			select {
			case err := <-done:
				if cancelRun && err != nil {
					t.Fatal(err)
				}
				if !cancelRun && !errors.Is(err, failure) {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("supervisor did not drain both tasks")
			}
			if err := client.Ping(context.Background()).Err(); err != nil {
				t.Fatal("example closed injected client", err)
			}
		})
	}
}
