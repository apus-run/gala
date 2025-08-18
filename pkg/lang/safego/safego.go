package safego

import (
	"context"
)

func Go(ctx context.Context, fn func()) {
	go func() {
		defer Recovery(ctx)
		fn()
	}()
}
