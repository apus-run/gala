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
// WatcherClient 是 Watcher 所需的发布/订阅能力。
// 它刻意不含命令执行与客户端持有权；底层客户端的关闭仍由调用方负责。
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
// Watcher 传递的是失效提示，不是策略本体，也不做投递确认。
// 一个实例绑定一个 Authorizer；注入的 client 仍归应用持有。
// Close 会等待正在执行的回调，切勿在回调里调用 Close。
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
// startup only; Close owns the subscription lifetime. Reconnects can lose
// events: applications must independently reconcile policy through periodic
// reloads.
// NewWatcher 返回前会等 Redis 确认订阅成功。
// 请为策略键/模型/租户的命名空间选用唯一的频道。ctx 只控制启动；
// 订阅生命周期归 Close 管理。重连可能丢事件：应用必须另行通过周期性
// 重载对账策略。
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

// SetUpdateCallback registers the callback invoked for each invalidation hint
// | SetUpdateCallback 注册收到失效提示时调用的回调.
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

// Update publishes an invalidation hint. A nil error does not mean peers
// loaded it successfully. Self messages are harmless full reloads; no identity
// protocol or implicit policy mutation is involved.
// Update 发布一条失效提示。返回 nil 不代表对端加载成功。
// 自己收到的消息只会触发一次无害的全量重载；没有身份协议，也不会隐式改策略。
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

// Close is idempotent and closes only the subscription, never the shared
// client. Stop external Update calls before closing.
// Close 幂等，只关闭订阅，绝不关闭共享的 client。
// 关闭前请先停掉外部的 Update 调用。
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
