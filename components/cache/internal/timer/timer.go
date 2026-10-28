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

// Timestamp returns the current time.
// Make sure to start the updater once using StartTimeStampUpdater() before calling.
func Timestamp() int64 {
	return timestamp.Load()
}

// ExpireAt 返回 exp 之后的过期时间戳（秒）；exp<=0 返回 0，表示永不过期。
//
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

// StartTimeStampUpdater starts a concurrent function which stores the timestamp to an atomic value per second,
// which is much better for performance than determining it at runtime each time
func StartTimeStampUpdater() {
	timestampTimer.Do(func() {
		// set initial value
		timestamp.Store(int64(time.Now().Unix()))
		go func(sleep time.Duration) {
			ticker := time.NewTicker(sleep)
			defer ticker.Stop()

			for t := range ticker.C {
				// update timestamp
				timestamp.Store(int64(t.Unix()))
			}
		}(1 * time.Second) // duration
	})
}
