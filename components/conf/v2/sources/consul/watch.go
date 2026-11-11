package consul

import (
	"context"
	"errors"

	"github.com/apus-run/gala/components/conf/v2/internal/backoff"
	"github.com/apus-run/gala/components/conf/v2/internal/remote"
)

// Watch signals after each successful baseline and index change. Requests and
// retry waits end when ctx is canceled, then the buffered channel closes.
func (s *Source) Watch(ctx context.Context) (<-chan struct{}, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !s.valid() {
		return nil, errors.New("invalid consul source")
	}
	ch := make(chan struct{}, 1)
	go func() {
		defer close(ch)
		var retry backoff.Delay
		var wait, previous uint64
		for ctx.Err() == nil {
			_, index, err := s.query(ctx, wait)
			switch {
			case err != nil:
				wait = 0
				if !retry.Wait(ctx) {
					return
				}
			case wait != 0 && index == 0:
				remote.Signal(ch)
				wait, previous = 1, 0
				if !retry.Wait(ctx) {
					return
				}
			case wait != 0 && index < previous:
				wait = 0
			default:
				if index > 0 {
					retry = 0
				}
				if wait == 0 || index != previous {
					remote.Signal(ch)
				}
				previous, wait = index, max(index, 1)
			}
		}
	}()
	return ch, nil
}
