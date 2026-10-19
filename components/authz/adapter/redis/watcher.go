package redisadapter

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/casbin/casbin/v3/persist"
	"github.com/redis/go-redis/v9"
)

// WatcherClient is the publish/subscribe capability required by Watcher.
// It deliberately excludes command execution and client ownership. The caller
// retains responsibility for closing the underlying client.
type WatcherClient interface {
	Publish(context.Context, string, any) *redis.IntCmd
	Subscribe(context.Context, ...string) *redis.PubSub
}

var (
	_ WatcherClient = (*redis.Client)(nil)
	_ WatcherClient = (*redis.ClusterClient)(nil)
)

var _ persist.Watcher = (*Watcher)(nil)

// Watcher delivers invalidation hints, not policies or delivery acknowledgements.
// One instance is bound to one Authorizer. The injected client remains app-owned.
// Close waits for an in-flight callback; never call Close from that callback.
type Watcher struct {
	client   WatcherClient
	channel  string
	sub      *redis.PubSub
	mu       sync.RWMutex
	callback func(string)
	closed   bool
	once     sync.Once
	stop     chan struct{}
	done     chan struct{}
}

// NewWatcher waits for Redis to acknowledge subscription before returning.
// Use a channel unique to the policy key/model/tenant namespace. ctx controls
// startup only; Close owns the subscription lifetime. Reconnects can lose events:
// applications must independently reconcile policy through periodic reloads.
func NewWatcher(ctx context.Context, client WatcherClient, channel string) (*Watcher, error) {
	if ctx == nil || nilClient(client) || channel == "" {
		return nil, errors.New("redis watcher: context, client and channel are required")
	}
	sub := client.Subscribe(ctx, channel)
	if _, err := sub.Receive(ctx); err != nil {
		_ = sub.Close()
		return nil, err
	}
	w := &Watcher{client: client, channel: channel, sub: sub, stop: make(chan struct{}), done: make(chan struct{})}
	messages := sub.Channel(redis.WithChannelSize(1))
	go func() {
		defer close(w.done)
		for {
			select {
			case <-w.stop:
				return
			case msg, ok := <-messages:
				if !ok {
					return
				}
				w.mu.RLock()
				cb, closed := w.callback, w.closed
				w.mu.RUnlock()
				if !closed && cb != nil {
					cb(msg.Payload)
				}
			}
		}
	}()
	return w, nil
}

func (w *Watcher) SetUpdateCallback(cb func(string)) error {
	if cb == nil {
		return errors.New("redis watcher: nil callback")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return errors.New("redis watcher: closed")
	}
	w.callback = cb
	return nil
}

// Update publishes an invalidation hint. A nil error does not mean peers loaded
// it successfully. Self messages are harmless full reloads; no identity protocol
// or implicit policy mutation is involved.
func (w *Watcher) Update() error {
	w.mu.RLock()
	closed := w.closed
	w.mu.RUnlock()
	if closed {
		return errors.New("redis watcher: closed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return w.client.Publish(ctx, w.channel, "policy changed").Err()
}

// Close is idempotent and closes only the subscription, never the shared client.
// Stop external Update calls before closing.
func (w *Watcher) Close() {
	w.once.Do(func() {
		w.mu.Lock()
		w.closed = true
		w.callback = nil
		w.mu.Unlock()
		close(w.stop)
		_ = w.sub.Close()
	})
	<-w.done
}
