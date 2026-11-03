// Package timer caches the current unix-second timestamp, refreshed once per
// second by a single background goroutine | timer 缓存秒级时间戳，由单个后台协程每秒刷新.
//
// # Notes | 说明
//
// [ExpireAt] rounds sub-second TTLs up to whole seconds, so expiry is
// second-granular only; call [StartTimeStampUpdater] once first or
// [Timestamp] stays 0. Internal package for components/cache.
// [ExpireAt] 将亚秒 TTL 向上取整为整秒，过期精度只有秒级；先调用一次
// [StartTimeStampUpdater]，否则 [Timestamp] 恒为 0。components/cache 内部包。
package timer

import (
	"sync"
	"sync/atomic"
	"time"
)

var (
	timestampTimer sync.Once
	timestamp      atomic.Int64
)

// Timestamp returns the cached current time in unix seconds. Start the updater
// once with StartTimeStampUpdater before calling, or the value stays 0.
// Timestamp 返回当前缓存的时间戳（unix 秒）。调用前必须先用
// StartTimeStampUpdater() 启动一次更新器，否则值恒为 0。
func Timestamp() int64 {
	return timestamp.Load()
}

// ExpireAt returns the unix-second expiry timestamp after exp; exp<=0 returns
// 0, meaning never expires.
// ExpireAt 返回 exp 之后的过期时间戳（秒）；exp<=0 返回 0，表示永不过期。
//
// The timestamp is refreshed only once per second (see StartTimeStampUpdater),
// so the TTL is second-granular only: exp is rounded up to c=⌈exp⌉ seconds and
// the real lifetime lands in (c-1s, c], depending on where the write falls
// between two updates. Rounding up only guarantees a sub-second TTL does not
// expire on write (with rounding down, a 500ms TTL would live 0 seconds); it
// does not guarantee a full exp: a 500ms TTL may live only a few dozen
// milliseconds. Use redis when a precise short TTL is needed.
// 时间戳每秒才更新一次（见 StartTimeStampUpdater），所以 TTL 只有秒级精度：
// 先向上取整为 c=⌈exp⌉ 秒，实际寿命落在 (c-1s, c] 之间，取决于写入发生在两次更新
// 之间的哪个位置。向上取整只保证亚秒 TTL 不会写入即过期（向下取整时 500ms 的 TTL
// 活 0 秒）；它不保证活满 exp：500ms 的 TTL 可能只活几十毫秒。
// 需要精确短 TTL 时用 redis。
func ExpireAt(exp time.Duration) int64 {
	if exp <= 0 {
		return 0
	}
	return Timestamp() + int64((exp+time.Second-1)/time.Second)
}

// StartTimeStampUpdater starts a background goroutine that stores the timestamp
// into an atomic value once per second, which performs far better than
// computing it on every call.
// StartTimeStampUpdater 启动一个后台协程，每秒把时间戳存入原子变量，
// 比每次调用时现算性能好得多。
func StartTimeStampUpdater() {
	timestampTimer.Do(func() {
		// set the initial value
		// 写入初始值
		timestamp.Store(int64(time.Now().Unix()))
		go func(sleep time.Duration) {
			ticker := time.NewTicker(sleep)
			defer ticker.Stop()

			for t := range ticker.C {
				// update the cached timestamp
				// 更新缓存的时间戳
				timestamp.Store(int64(t.Unix()))
			}
		}(1 * time.Second) // duration | 每秒一跳
	})
}
