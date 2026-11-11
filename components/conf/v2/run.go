package conf

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/apus-run/gala/components/conf/v2/internal/backoff"
)

// Watcher signals that the caller should reload all layers. Watch must honor
// its context and release resources when canceled.
type Watcher interface {
	Watch(context.Context) (<-chan struct{}, error)
}

// RunOptions configures the explicitly started event loop. Zero durations
// select a 75ms quiet interval and a 1s to 30s exponential retry ceiling.
type RunOptions struct {
	Debounce     time.Duration
	RetryInitial time.Duration
	RetryMax     time.Duration
	// OnError sees each failed reload before retry. With nil, Run returns
	// the first failure. The callback runs synchronously outside the transaction.
	OnError func(error)
}

// Run starts each watcher and synchronously drives full reloads until ctx
// ends or all channels close. A quiet debounce interval coalesces bursts; events
// during a reload schedule another attempt. When all watchers close, Run
// finishes pending work once. Setup failure cancels already started watchers.
func Run(ctx context.Context, cfg interface{ Reload(context.Context) error }, opts RunOptions, watchers ...Watcher) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if isNil(cfg) || len(watchers) == 0 {
		return errors.New("reload target and watchers required")
	}
	if opts.Debounce == 0 {
		opts.Debounce = 75 * time.Millisecond
	}
	if opts.RetryInitial == 0 {
		opts.RetryInitial = time.Second
	}
	if opts.RetryMax == 0 {
		opts.RetryMax = 30 * time.Second
	}
	if opts.Debounce < 0 || opts.RetryInitial <= 0 || opts.RetryMax < opts.RetryInitial {
		return errors.New("invalid reload interval")
	}

	ctx, cancel := context.WithCancel(ctx)
	signals := make(chan struct{}, 1)
	var workers sync.WaitGroup
	defer func() {
		cancel()
		workers.Wait()
	}()
	for _, w := range watchers {
		if isNil(w) {
			return errors.New("nil watcher")
		}
		ch, err := w.Watch(ctx)
		if err != nil {
			return err
		}
		if ch == nil {
			return errors.New("watcher returned nil channel")
		}
		workers.Go(func() {
			for {
				select {
				case <-ctx.Done():
					return
				case _, ok := <-ch:
					if !ok {
						return
					}
					select {
					case signals <- struct{}{}:
					default:
					}
				}
			}
		})
	}
	go func() {
		workers.Wait()
		close(signals)
	}()

	var timer *time.Timer
	var tick <-chan time.Time
	var retry backoff.Delay
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case _, ok := <-signals:
			if !ok {
				if err := ctx.Err(); err != nil {
					return err
				}
				if tick == nil {
					return nil
				}
				// No more invalidations can arrive. Report the final outcome.
				err := cfg.Reload(ctx)
				if cause := ctx.Err(); cause != nil {
					return cause
				}
				if err != nil && opts.OnError != nil {
					opts.OnError(err)
				}
				return err
			}
			if retry == 0 {
				if timer == nil {
					timer = time.NewTimer(opts.Debounce)
				} else {
					timer.Reset(opts.Debounce)
				}
				tick = timer.C
			}
		case <-tick:
			if err := ctx.Err(); err != nil {
				return err
			}
			err := cfg.Reload(ctx)
			if err == nil {
				retry = 0
				tick = nil
				continue
			}
			if cause := ctx.Err(); cause != nil {
				return cause
			}
			if opts.OnError == nil {
				return err
			}
			opts.OnError(err)
			timer.Reset(retry.Next(opts.RetryInitial, opts.RetryMax))
		}
	}
}
