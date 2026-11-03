// Package rmap provides a replicated map: every process that joins the same
// name holds an in-memory copy of a Redis hash and receives changes over
// pub/sub | 复制 map：Join 同名的每个进程持有 Redis hash 的内存副本，经 pub/sub 收变更.
//
// # Notes | 说明
//
// Mutations run as Lua scripts that update the hash and publish in one atomic
// step, so all nodes stay eventually consistent. Subscribe only signals that
// something changed — several remote changes may collapse into one
// notification; read the new content with Map. After a Redis disconnect the
// map reconnects forever; after Close it becomes a read-only point-in-time
// copy. Keys may not contain "=", and list values are stored comma-separated,
// so list items may not contain commas. Only a standalone *redis.Client is
// supported, not a cluster client.
// 变更由 Lua 脚本原子地更新 hash 并发布，所有节点最终一致。Subscribe 只通知
// “发生了变化”—— 多次远端变更可能合并为一次通知，新内容要用 Map 读取。Redis
// 断连后会无限重连；Close 之后变为只读的时点快照。key 不能含 “=”，列表值以逗号
// 拼接存储，列表元素不能含逗号。仅支持独立部署的 *redis.Client，不支持集群客户端。
//
// # Features | 功能
//
// - Join by name: snapshot first, then live updates | 按名字 Join：先快照再实时更新
// - Set / TestAndSet / SetIfNotExists / Delete / TestAndDelete | 条件写与条件删
// - Inc counters; AppendValues / RemoveValues comma lists | Inc 计数与逗号列表追加/移除
// - Reset / TestAndReset the whole map | 整表清空与条件清空
// - Subscribe change notifications; SetAndWait for own writes | 变更订阅与等待自身写入生效
//
// # Example | 示例
//
//	m, err := rmap.Join(ctx, "mymap", redisClient)
//	if err != nil {
//		return err
//	}
//	defer m.Close()
//	_, _ = m.Set(ctx, "color", "blue")
//	v, ok := m.Get("color")
package rmap

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"maps"
	"math/rand"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/apus-run/gala/pkg/lang/safego"
)

type (
	// Map is a replicated map that emits events when elements
	// change. Multiple processes can join the same replicated map and
	// update it.
	// Map 是元素变更时发出事件的复制 map。多个进程可加入同名复制 map 并更新它.
	Map struct {
		Name                 string
		chankey              string                // Redis pubsub channel name | Redis pub/sub 频道名
		hashkey              string                // Redis hash key | Redis hash 键
		msgch                <-chan *redis.Message // channel to receive map updates | 接收 map 更新的频道
		chans                []chan EventKind      // channels to send notifications | 发送通知的频道列表
		done                 chan struct{}         // channel to signal shutdown | 通知关闭的频道
		wait                 sync.WaitGroup        // wait for read goroutine to exit | 等待读 goroutine 退出
		sub                  *redis.PubSub         // subscription to map updates | map 更新的订阅
		rdb                  *redis.Client
		setScript            *redis.Script
		testAndSetScript     *redis.Script
		setIfNotExistsScript *redis.Script
		incrScript           *redis.Script
		appendScript         *redis.Script
		appendUniqueScript   *redis.Script
		removeScript         *redis.Script
		delScript            *redis.Script
		testAndDelScript     *redis.Script
		testAndResetScript   *redis.Script
		resetScript          *redis.Script

		lock    sync.RWMutex
		content map[string]string
		closing bool // true if Close was called | 已调用 Close 则为 true
		closed  bool // true if Close returned - used by tests | Close 已返回则为 true（测试用）

		wlock   sync.Mutex
		waiters sync.Map // map of key to []*setWaiter | key 到 []*setWaiter 的映射
	}

	// EventKind is the type of map event | EventKind 是 map 事件的类型.
	EventKind int

	// setNotification is the type of internal notification sent when a key is set | setNotification 是 key 被设置时发送的内部通知类型.
	setNotification struct {
		key   string
		value string
	}

	// setWaiter represents a waiting SetAndWait operation | setWaiter 表示一次等待中的 SetAndWait 操作.
	setWaiter struct {
		ch     chan setNotification
		key    string
		value  string
		ctx    context.Context // context for cancellation | 用于取消的 ctx
		cancel func()          // cleanup function | 清理函数
	}
)

const (
	// EventChange is the event emitted when a key is added or changed | key 新增或变更时发出 EventChange.
	EventChange EventKind = iota + 1
	// EventDelete is the event emitted when a key is deleted | key 被删除时发出 EventDelete.
	EventDelete
	// EventReset is the event emitted when the map is reset | map 被重置时发出 EventReset.
	EventReset
)

// Join retrieves the content of the replicated map with the given name and
// subscribes to updates. The local content is eventually consistent across all
// nodes that join the replicated map with the same name.
//
// Clients can call the Map method on the returned Map to retrieve a copy of
// its content and subscribe to its C channel to receive updates when the
// content changes (note that multiple remote changes may result in a single
// notification). The returned Map is safe for concurrent use.
//
// Clients should call Close before exiting to stop updates and release
// resources resulting in a read-only point-in-time copy.
// Join 取回指定名字复制 map 的内容并订阅更新。同名加入复制 map 的所有节点最终一致。
// 客户端可调用返回 Map 的 Map 方法获取内容副本，并通过 Subscribe 订阅内容变更通知
// （注意多次远端变更可能合并为一次通知）。返回的 Map 可安全并发使用。
// 客户端应在退出前调用 Close 停止更新并释放资源，此后成为只读的时点快照.
func Join(ctx context.Context, name string, rdb *redis.Client) (*Map, error) {
	if !isValidRedisKeyName(name) {
		return nil, fmt.Errorf("pulse map: not a valid map name %q", name)
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	sm := &Map{
		Name:                 name,
		chankey:              fmt.Sprintf("map:%s:updates", name),
		hashkey:              fmt.Sprintf("map:%s:content", name),
		done:                 make(chan struct{}),
		rdb:                  rdb,
		content:              make(map[string]string),
		setScript:            luaSet,
		testAndSetScript:     luaTestAndSet,
		setIfNotExistsScript: luaSetIfNotExists,
		incrScript:           luaIncr,
		appendScript:         luaAppend,
		appendUniqueScript:   luaAppendUnique,
		removeScript:         luaRemove,
		delScript:            luaDelete,
		testAndDelScript:     luaTestAndDel,
		testAndResetScript:   luaTestAndReset,
		resetScript:          luaReset,
	}
	if err := sm.init(ctx); err != nil {
		return nil, err
	}

	// read updates
	// 读取更新
	sm.wait.Add(1)
	safego.Go(ctx, sm.run)

	return sm, nil
}

// Map returns a copy of the replicated map content | Map 返回复制 map 内容的副本.
func (sm *Map) Map() map[string]string {
	sm.lock.RLock()
	defer sm.lock.RUnlock()

	hash := make(map[string]string, len(sm.content))
	maps.Copy(hash, sm.content)
	return hash
}

// Keys returns a copy of the replicated map keys | Keys 返回复制 map 所有 key 的副本.
func (sm *Map) Keys() []string {
	sm.lock.RLock()
	defer sm.lock.RUnlock()

	keys := make([]string, 0, len(sm.content))
	for k := range sm.content {
		keys = append(keys, k)
	}
	return keys
}

// Subscribe returns a channel that receives notifications when the map
// changes. The channel is closed when the map is stopped. This channel simply
// notifies that the map has changed, it does not provide the actual changes,
// instead the Map method should be used to read the current content.  This
// allows the notification to be sent without blocking. Multiple remote updates
// may result in a single notification.
// Subscribe returns nil if the map is stopped.
// Subscribe 返回 map 变更时接收通知的频道，map 停止时频道关闭。该频道只通知
// “map 已变化”，不携带具体变更；当前内容应改用 Map 方法读取。这使通知可以非阻塞地
// 发出，多次远端更新可能合并为一次通知。map 已停止时 Subscribe 返回 nil.
func (sm *Map) Subscribe() <-chan EventKind {
	sm.lock.Lock()
	defer sm.lock.Unlock()
	if sm.closing {
		return nil
	}
	c := make(chan EventKind, 1) // Buffer 1 notification so we don't have to block. | 缓冲 1 条通知以免阻塞
	sm.chans = append(sm.chans, c)
	return c
}

// Unsubscribe removes the given channel from the list of subscribers and closes it | Unsubscribe 从订阅者列表移除并关闭给定频道.
func (sm *Map) Unsubscribe(c <-chan EventKind) {
	sm.lock.Lock()
	defer sm.lock.Unlock()
	if sm.closing {
		return
	}
	for i, ch := range sm.chans {
		if ch == c {
			close(sm.chans[i])
			sm.chans = append(sm.chans[:i], sm.chans[i+1:]...)
			return
		}
	}
}

// Len returns the number of items in the replicated map | Len 返回复制 map 的条目数.
func (sm *Map) Len() int {
	sm.lock.RLock()
	defer sm.lock.RUnlock()
	return len(sm.content)
}

// Get returns the value for the given key | Get 返回给定 key 的值.
func (sm *Map) Get(key string) (string, bool) {
	sm.lock.RLock()
	defer sm.lock.RUnlock()
	res, ok := sm.content[key]
	return res, ok
}

// GetValues returns the comma separated values for the given key.
// This is a convenience method intended to be used in conjunction with
// AppendValues and RemoveValues.
// GetValues 返回给定 key 按逗号拆分的值列表。它是配合 AppendValues 与
// RemoveValues 使用的便捷方法.
func (sm *Map) GetValues(key string) ([]string, bool) {
	val, ok := sm.Get(key)
	if !ok {
		return nil, false
	}
	return strings.Split(val, ","), true
}

// Set sets the value for the given key and returns the previous value.
// An error is returned if:
// - The key is empty
// - The key contains an equal sign
// - There's an issue with the Redis operation
//
// Example:
// Set(ctx, "color", "blue") would set the "color" key to "blue"
// and return the previous value, if any.
// Set 设置给定 key 的值并返回旧值。以下情况返回错误：key 为空；key 含等号；
// Redis 操作出错。
// 示例：Set(ctx, "color", "blue") 会把 "color" 设为 "blue" 并返回旧值（若有）.
func (sm *Map) Set(ctx context.Context, key, value string) (string, error) {
	prev, err := sm.runLuaScript(ctx, "set", sm.setScript, key, value)
	if err != nil {
		return "", err
	}
	if prev == nil {
		return "", nil
	}
	return prev.(string), nil
}

// SetAndWait is a convenience method that calls Set and waits for the update to be
// applied and the notification to be sent. Multiple concurrent calls with the same
// key and value are allowed - each call will receive its own notification when the
// update is applied.
//
// The method will return an error if:
// - The key is empty
// - The key contains an equal sign
// - The context is cancelled
// - The map is stopped
// - There's an issue with the Redis operation
// SetAndWait 是便捷方法：调用 Set 并等待更新生效、通知发出。相同 key 与 value 的
// 并发调用是允许的——更新生效时每次调用各自收到通知。以下情况返回错误：key 为空；
// key 含等号；ctx 被取消；map 已停止；Redis 操作出错.
func (sm *Map) SetAndWait(ctx context.Context, key, value string) (string, error) {
	notifyCh := make(chan setNotification, 1)

	// Create cancellable context for cleanup
	// 创建可取消的 ctx 以便清理
	waitCtx, cancel := context.WithCancel(ctx)
	waiter := &setWaiter{
		ch:     notifyCh,
		key:    key,
		value:  value,
		ctx:    waitCtx,
		cancel: cancel,
	}

	// First mark the channel as closing before removing from waiters
	// 先标记关闭，再从等待者列表移除
	defer func() {
		// Cancel first to prevent new sends
		// 先取消以阻止新的发送
		cancel()

		// Remove waiter under lock
		// 持锁移除等待者
		sm.wlock.Lock()
		if v, ok := sm.waiters.Load(key); ok {
			waiters := v.([]*setWaiter)
			newWaiters := make([]*setWaiter, 0, len(waiters))
			for _, w := range waiters {
				if w != waiter {
					newWaiters = append(newWaiters, w)
				}
			}
			if len(newWaiters) > 0 {
				sm.waiters.Store(key, newWaiters)
			} else {
				sm.waiters.Delete(key)
			}
		}
		sm.wlock.Unlock()

		// Do not close(notifyCh): run() sends to waiter.ch after releasing wlock (it holds a
		// waiter snapshot), racing with this would send on a closed channel and panic. The
		// sender's select also watches waiter.ctx.Done(); after cancel() above it will not
		// block; the channel is left to the GC once it has no readers.
		// 不能 close(notifyCh)：run() 在释放 wlock 之后才向 waiter.ch 发送（它取的是 waiter 快照），
		// 与这里并发就是向已关闭的 channel 发送，直接 panic。发送方的 select 里有 waiter.ctx.Done()，
		// 上面 cancel() 之后不会再阻塞；channel 没有读者之后交给 GC 回收。
	}()

	// Prepare new waiters list under lock
	// 持锁准备新的等待者列表
	sm.wlock.Lock()
	if v, ok := sm.waiters.Load(key); ok {
		waiters := v.([]*setWaiter)
		newWaiters := append(append([]*setWaiter(nil), waiters...), waiter) // Note: need to copy to avoid data race | 注意：需复制以防数据竞争
		sm.waiters.Store(key, newWaiters)
	} else {
		sm.waiters.Store(key, []*setWaiter{waiter})
	}
	sm.wlock.Unlock()

	// Call Set - if it fails, the deferred cleanup will remove our waiter
	// 调用 Set——失败时由 defer 清理移除等待者
	prev, err := sm.Set(ctx, key, value)
	if err != nil {
		return "", err
	}

	// Wait for notification or context cancellation
	// 等待通知或 ctx 取消
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-sm.done:
		return "", fmt.Errorf("pulse map: %s is stopped", sm.Name)
	case ev := <-notifyCh:
		if ev.key == key && ev.value == value {
			return prev, nil
		}
		// This shouldn't happen as we only send matching notifications
		// 不应发生：我们只发送匹配的通知
		return "", fmt.Errorf("pulse map: received unexpected notification key=%s value=%s", ev.key, ev.value)
	}
}

// SetIfNotExists sets the value for key only if it doesn't exist.
// Returns true if the value was set, false if the key already existed.
// SetIfNotExists 仅在 key 不存在时设置值。设置成功返回 true，key 已存在返回 false.
func (sm *Map) SetIfNotExists(ctx context.Context, key, value string) (bool, error) {
	v, err := sm.runLuaScript(ctx, "setIfNotExists", sm.setIfNotExistsScript, key, value)
	if err != nil {
		return false, err
	}
	return v.(int64) == 1, nil
}

// TestAndSet sets the value for the given key if the current value matches the
// given test value. The previous value is returned.
// An error is returned if:
// - The key is empty
// - The key contains an equal sign
// - There's an issue with the Redis operation
//
// Example:
// TestAndSet(ctx, "color", "red", "blue") would set "color" to "blue"
// only if its current value is "red", and return the previous value.
// TestAndSet 在当前值与 test 匹配时设置 value，返回旧值。以下情况返回错误：key 为空；
// key 含等号；Redis 操作出错。
// 示例：TestAndSet(ctx, "color", "red", "blue") 仅在 "color" 当前值为 "red" 时设为
// "blue"，并返回旧值.
func (sm *Map) TestAndSet(ctx context.Context, key, test, value string) (string, error) {
	prev, err := sm.runLuaScript(ctx, "testAndSet", sm.testAndSetScript, key, test, value)
	if err != nil {
		return "", err
	}
	if prev == nil {
		return "", nil
	}
	return prev.(string), nil
}

// Inc increments the value for the given key and returns the result.
// The value must represent an integer.
// An error is returned if:
// - The key is empty
// - The key contains an equal sign
// - The value does not represent an integer
// - There's an issue with the Redis operation
//
// Example:
// Inc(ctx, "counter", 1) would increment the "counter" by 1
// and return the new value.
// Inc 把给定 key 的值加上 delta 并返回结果，值必须是整数字符串。以下情况返回错误：
// key 为空；key 含等号；值不是整数；Redis 操作出错。
// 示例：Inc(ctx, "counter", 1) 把 "counter" 加 1 并返回新值.
func (sm *Map) Inc(ctx context.Context, key string, delta int) (int, error) {
	res, err := sm.runLuaScript(ctx, "incr", sm.incrScript, key, delta)
	if err != nil {
		return 0, err
	}
	v, err := strconv.ParseInt(res.(string), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("pulse map: %s: %s", key, err)
	}
	return int(v), nil
}

// AppendValues appends the given items to the value for the given key and
// returns the result. The array of items is stored as a comma-separated list.
// An error is returned if:
// - The key is empty
// - The key contains an equal sign
// - There's an issue with the Redis operation
//
// Example:
// AppendValues(ctx, "fruits", "apple", "banana") would append "apple" and "banana"
// to the existing list of fruits and return the updated list.
// AppendValues 把 items 追加到给定 key 的值后并返回结果，列表以逗号分隔存储。
// 以下情况返回错误：key 为空；key 含等号；Redis 操作出错。
// 示例：AppendValues(ctx, "fruits", "apple", "banana") 把 "apple" 和 "banana" 追加到
// 既有水果列表并返回更新后的列表.
func (sm *Map) AppendValues(ctx context.Context, key string, items ...string) ([]string, error) {
	sitems := strings.Join(items, ",")
	res, err := sm.runLuaScript(ctx, "append", sm.appendScript, key, sitems)
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, nil
	}
	return strings.Split(res.(string), ","), nil
}

// AppendUniqueValues appends the given items to the value for the given key if
// they are not already present and returns the result. The array of items is
// stored as a comma-separated list.
// An error is returned if:
// - The key is empty
// - The key contains an equal sign
// - There's an issue with the Redis operation
//
// Example:
// AppendUniqueValues(ctx, "fruits", "apple", "banana") would append only unique values
// to the existing list of fruits and return the updated list.
// AppendUniqueValues 仅在 items 尚不存在时追加到给定 key 的值后并返回结果，
// 列表以逗号分隔存储。错误条件同 AppendValues.
// 示例：AppendUniqueValues(ctx, "fruits", "apple", "banana") 只追加不重复的值并返回
// 更新后的列表.
func (sm *Map) AppendUniqueValues(ctx context.Context, key string, items ...string) ([]string, error) {
	sitems := strings.Join(items, ",")
	res, err := sm.runLuaScript(ctx, "appendUnique", sm.appendUniqueScript, key, sitems)
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, nil
	}
	return strings.Split(res.(string), ","), nil
}

// RemoveValues removes the given items from the value for the given key and
// returns the remaining values after removal. The function behaves as follows:
//
//  1. The value for the key is expected to be a comma-separated list of items.
//  2. It removes all occurrences of the specified items from this list.
//  3. If the removal results in an empty list, the key is automatically deleted.
//  4. Returns the remaining items as a slice of strings, a boolean indicating
//     whether any value was removed, and an error (if any).
//  5. If the key doesn't exist, it returns nil, false, nil.
//
// An error is returned if:
// - The key is empty
// - The key contains an equal sign
// - There's an issue with the Redis operation
//
// Example:
// Given a key "fruits" with value "apple,banana,cherry,apple"
// RemoveValues(ctx, "fruits", "apple", "cherry") would return (["banana"], true, nil)
// and update the value in Redis to "banana"
// RemoveValues 从给定 key 的值中移除 items，返回移除后的剩余值。行为：值应为逗号分隔
// 列表；移除其中所有出现的指定项；移除后为空则自动删除该 key；返回剩余项切片、是否有
// 移除的布尔值与错误；key 不存在时返回 nil, false, nil。错误条件：key 为空；key 含等号；
// Redis 操作出错.
// 示例：key "fruits" 值为 "apple,banana,cherry,apple" 时，RemoveValues(ctx, "fruits",
// "apple", "cherry") 返回 (["banana"], true, nil)，Redis 中的值更新为 "banana".
func (sm *Map) RemoveValues(ctx context.Context, key string, items ...string) ([]string, bool, error) {
	sitems := strings.Join(items, ",")
	res, err := sm.runLuaScript(ctx, "remove", sm.removeScript, key, sitems)
	if err != nil {
		return nil, false, err
	}
	result := res.([]any)
	if result[0] == nil {
		return nil, false, nil // Key didn't exist | key 原不存在
	}
	remaining := result[0].(string)
	if remaining == "" {
		return nil, true, nil // All items were removed, key was deleted | 所有项被移除，key 已删除
	}
	removed := result[1] != nil && result[1].(int64) == 1
	return strings.Split(remaining, ","), removed, nil
}

// Delete deletes the value for the given key and returns the previous value.
// An error is returned if:
// - The key is empty
// - The key contains an equal sign
// - There's an issue with the Redis operation
//
// Example:
// Delete(ctx, "color") would delete the "color" key and return its previous value, if any.
// Delete 删除给定 key 并返回旧值。以下情况返回错误：key 为空；key 含等号；
// Redis 操作出错.
// 示例：Delete(ctx, "color") 删除 "color" key 并返回其旧值（若有）.
func (sm *Map) Delete(ctx context.Context, key string) (string, error) {
	prev, err := sm.runLuaScript(ctx, "delete", sm.delScript, key)
	if err != nil {
		return "", err
	}
	if prev == nil {
		return "", nil
	}
	return prev.(string), nil
}

// TestAndDelete tests that the value for the given key matches the test value
// and deletes the key if it does. It returns the previous value.
// An error is returned if:
// - The key is empty
// - The key contains an equal sign
// - There's an issue with the Redis operation
//
// Example:
// TestAndDelete(ctx, "color", "blue") would delete the "color" key only if
// its current value is "blue", and return the previous value.
// TestAndDelete 校验给定 key 的当前值与 test 匹配时删除该 key，返回旧值。错误条件：
// key 为空；key 含等号；Redis 操作出错.
// 示例：TestAndDelete(ctx, "color", "blue") 仅在 "color" 当前值为 "blue" 时删除并返回旧值.
func (sm *Map) TestAndDelete(ctx context.Context, key, test string) (string, error) {
	prev, err := sm.runLuaScript(ctx, "testAndDelete", sm.testAndDelScript, key, test)
	if err != nil {
		return "", err
	}
	if prev == nil {
		return "", nil
	}
	return prev.(string), nil
}

// Reset clears the map content. Reset is the only method that can be called
// after the map is closed.
// Reset 清空 map 内容。Reset 是 map 关闭后唯一可调用的方法.
func (sm *Map) Reset(ctx context.Context) error {
	_, err := sm.runLuaScript(ctx, "reset", sm.resetScript, "*")
	return err
}

// TestAndReset tests that the values for the given keys match the test values
// and clears the map if they do. It returns true if the map was cleared, false otherwise.
// An error is returned if:
// - Any key is empty
// - Any key contains an equal sign
// - There's an issue with the Redis operation
//
// Example:
// TestAndReset(ctx, []string{"color", "size"}, []string{"blue", "large"}) would clear the map
// only if the "color" key has value "blue" and the "size" key has value "large",
// and return true if the map was cleared.
// TestAndReset 校验给定 keys 的值与 tests 匹配时清空 map，清空则返回 true。错误条件：
// 任一 key 为空；任一 key 含等号；Redis 操作出错.
// 示例：TestAndReset(ctx, []string{"color", "size"}, []string{"blue", "large"}) 仅在
// "color" 为 "blue" 且 "size" 为 "large" 时清空 map，并返回是否清空.
func (sm *Map) TestAndReset(ctx context.Context, keys, tests []string) (bool, error) {
	args := make([]any, 1+len(keys)+len(tests))
	args[0] = "*"
	for i, k := range keys {
		args[i+1] = k
	}
	for i, t := range tests {
		args[len(keys)+i+1] = t
	}
	res, err := sm.runLuaScript(ctx, "testAndReset", sm.testAndResetScript, args...)
	if err != nil {
		return false, err
	}
	return res.(int64) == 1, nil
}

// Close closes the connection to the map, freeing resources. It is safe to
// call Close multiple times.
// Close 关闭与 map 的连接并释放资源，可安全重复调用.
func (sm *Map) Close() {
	sm.lock.Lock()
	if sm.closing {
		sm.lock.Unlock()
		return
	}
	sm.closing = true
	sm.lock.Unlock()

	// Signal run() to stop and wait for it to complete
	// 通知 run() 停止并等待其完成
	close(sm.done)
	sm.wait.Wait()

	// Clean up all waiters
	// 清理所有等待者
	sm.wlock.Lock()
	sm.waiters.Range(func(key, value any) bool {
		waiters := value.([]*setWaiter)
		for _, w := range waiters {
			w.cancel()
		}
		return true
	})
	sm.wlock.Unlock()
	sm.lock.Lock()
	sm.closed = true
	sm.lock.Unlock()
}

// init initializes the map | init 初始化 map.
func (sm *Map) init(ctx context.Context) error {
	// Make sure scripts are cached.
	// 确保 Lua 脚本已缓存.
	for _, script := range []*redis.Script{
		sm.appendScript,
		sm.appendUniqueScript,
		sm.delScript,
		sm.incrScript,
		sm.removeScript,
		sm.resetScript,
		sm.setScript,
		sm.testAndDelScript,
		sm.testAndResetScript,
		sm.testAndSetScript,
		sm.setIfNotExistsScript,
	} {
		if err := script.Load(ctx, sm.rdb).Err(); err != nil {
			return fmt.Errorf("pulse map: %s failed to load Lua scripts %v: %w", sm.Name, script, err)
		}
	}

	// Subscribe to updates.
	// 订阅更新.
	sm.sub = sm.rdb.Subscribe(ctx, sm.chankey)
	_, err := sm.sub.Receive(ctx) // Fail fast if we can't subscribe.
	if err != nil {
		return fmt.Errorf("pulse map: %s failed to join: %w", sm.Name, err)
	}
	sm.msgch = sm.sub.Channel()

	// read initial content
	// 读取初始内容
	// Note: there's a (very) small window where we might be receiving
	// updates for changes that are already applied by the time we call
	// HGetAll. This is not a problem because we'll just overwrite the
	// local copy with the same data.
	// 注意：存在一个（很）小的窗口，收到的更新可能对应 HGetAll 之前已应用的变更。
	// 这不是问题——本地副本会被相同数据覆盖.
	cmd := sm.rdb.HGetAll(ctx, sm.hashkey)
	if err := cmd.Err(); err != nil {
		return fmt.Errorf("pulse map: %s failed to read initial content: %w", sm.Name, err)
	}
	sm.content = cmd.Val()

	return nil
}

// run updates the local copy of the replicated map whenever a remote update is
// received and sends notifications when needed.
// run 在收到远端更新时更新本地副本，并在需要时发出通知.
func (sm *Map) run() {
	for {
		select {
		case msg, ok := <-sm.msgch:
			if !ok {
				// disconnected from Redis server, attempt to reconnect forever
				// 与 Redis 服务端断连，无限重连
				slog.Error("disconnected")
				sm.reconnect()
				continue
			}
			parts := strings.SplitN(msg.Payload, ":", 2)
			if len(parts) != 2 {
				slog.Error("invalid payload", "payload", msg.Payload)
				continue
			}
			op, data := parts[0], []byte(parts[1])
			sm.lock.Lock()
			kind := EventChange
			var notification *setNotification
			switch op {
			case "reset":
				sm.content = make(map[string]string)
				slog.Debug("reset")
				kind = EventReset
			case "del":
				key, _, err := unpackString(data)
				if err != nil {
					slog.Error("invalid del payload", "payload", msg.Payload, "error", err)
					sm.lock.Unlock()
					continue
				}
				delete(sm.content, key)
				slog.Debug("deleted", "key", key)
				kind = EventDelete
			case "set":
				key, rest, err := unpackString(data)
				if err != nil {
					slog.Error("invalid set key", "payload", msg.Payload, "error", err)
					sm.lock.Unlock()
					continue
				}
				val, _, err := unpackString(rest)
				if err != nil {
					slog.Error("invalid set value", "payload", msg.Payload, "error", err)
					sm.lock.Unlock()
					continue
				}
				sm.content[key] = val
				notification = &setNotification{key: key, value: val}
				slog.Debug("set", "key", key, "val", val)
			}

			for _, c := range sm.chans {
				select {
				case c <- kind:
				default:
				}
			}
			sm.lock.Unlock()

			// For set operations, notify waiters after releasing lock
			// set 操作在释放锁之后再通知等待者
			if notification != nil {
				// Load waiters for this key
				// 加载该 key 的等待者
				if w, ok := sm.waiters.Load(notification.key); ok {
					// Take lock only while reading waiters list
					// 仅在读取等待者列表时持锁
					sm.wlock.Lock()
					waiters := w.([]*setWaiter)
					sm.wlock.Unlock()

					// Notify matching waiters - no lock needed as each waiter has its own channel
					// 通知匹配的等待者——各自持有频道，无需加锁
					for _, waiter := range waiters {
						if waiter.key == notification.key && waiter.value == notification.value {
							select {
							case waiter.ch <- *notification:
							case <-sm.done:
								sm.shutdownRun()
								return
							case <-waiter.ctx.Done():
								// Waiter was cancelled or timed out
								// 等待者已取消或超时
								continue
							}
						}
					}
				}
			}

		case <-sm.done:
			sm.shutdownRun()
			return
		}
	}
}

// shutdownRun is the only exit path of run(): release the subscription and notify Close.
// shutdownRun 是 run() 的唯一退出路径：释放订阅并通知 Close.
// Every return must go through it first, otherwise sm.wait.Done() is skipped and Close blocks forever.
// 任何 return 都必须先经过它，否则 sm.wait.Done() 被跳过，Close 会永久阻塞.
func (sm *Map) shutdownRun() {
	slog.Info("closed")
	// no need to lock, stopping is true
	// 无需加锁，stopping 已为 true
	for _, c := range sm.chans {
		close(c)
	}
	if err := sm.sub.Unsubscribe(context.Background(), sm.chankey); err != nil {
		slog.Error("failed to unsubscribe", slog.Any("err", err))
	}
	if err := sm.sub.Close(); err != nil {
		slog.Error("failed to close subscription", slog.Any("err", err))
	}
	sm.wait.Done()
}

// runLuaScript runs the given Lua script, the first argument must be the key.
// It is the caller's responsibility to make sure the map is locked.
// runLuaScript 运行给定 Lua 脚本，第一个参数必须是 key。调用方需自行保证加锁.
func (sm *Map) runLuaScript(ctx context.Context, name string, script *redis.Script, args ...any) (any, error) {
	sm.lock.RLock()
	if sm.closing && name != "reset" {
		sm.lock.RUnlock()
		return "", fmt.Errorf("pulse map: %s is stopped", sm.Name)
	}
	sm.lock.RUnlock()
	key := args[0].(string)
	if len(key) == 0 {
		return nil, fmt.Errorf("pulse map: %s key cannot be empty in %q", sm.Name, name)
	}
	if strings.Contains(key, "=") {
		return nil, fmt.Errorf("pulse map: %s key %q cannot contain '=' in %q", sm.Name, key, name)
	}
	res, err := script.EvalSha(ctx, sm.rdb, []string{sm.hashkey, sm.chankey}, args...).Result()
	if err != nil && err != redis.Nil {
		return nil, fmt.Errorf("pulse map: %s failed to run %q for key %s: %w", sm.Name, name, key, err)
	}

	return res, nil
}

// reconnect attempts to reconnect to the Redis server forever | reconnect 无限尝试重连 Redis 服务端.
func (sm *Map) reconnect() {
	var count int
	for {
		count++
		slog.Info("reconnect", "attempt", count)
		sm.lock.Lock()
		if sm.closing {
			sm.lock.Unlock()
			return
		}
		sm.sub = sm.rdb.Subscribe(context.Background(), sm.chankey)
		_, err := sm.sub.Receive(context.Background())
		if err != nil {
			sm.lock.Unlock()
			slog.Error("failed to reconnect", slog.Any("err", err), "attempt", count)
			time.Sleep(time.Duration(rand.Float64()*5+1) * time.Second)
			continue
		}
		sm.msgch = sm.sub.Channel()
		sm.lock.Unlock()
		slog.Info("reconnected")
		break
	}
}

// redisKeyRegex is a regular expression that matches valid Redis keys | redisKeyRegex 是匹配合法 Redis key 的正则.
var redisKeyRegex = regexp.MustCompile(`^[^ \0\*\?\[\]]{1,512}$`)

func isValidRedisKeyName(key string) bool {
	return redisKeyRegex.MatchString(key)
}

// unpackString reads a length-prefixed string from a buffer using struct.pack
// format "ic0"
// unpackString 按 struct.pack "ic0" 格式从缓冲读取带长度前缀的字符串.
func unpackString(data []byte) (string, []byte, error) {
	if len(data) < 4 {
		return "", nil, fmt.Errorf("buffer too short for length")
	}
	length := int(binary.LittleEndian.Uint32(data))
	data = data[4:]
	if len(data) < length {
		return "", nil, fmt.Errorf("buffer too short for string")
	}
	return string(data[:length]), data[length:], nil
}
