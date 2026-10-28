package timer

import (
	"testing"
	"time"
)

// 秒级时间戳下 TTL 必须向上取整：向下取整会让亚秒 TTL 写入即过期。
func TestExpireAt(t *testing.T) {
	StartTimeStampUpdater()
	now := Timestamp()
	for _, tc := range []struct {
		exp  time.Duration
		want int64
	}{
		{0, 0},
		{-time.Second, 0},
		{time.Nanosecond, now + 1},
		{500 * time.Millisecond, now + 1},
		{time.Second, now + 1},
		{1500 * time.Millisecond, now + 2},
		{time.Minute, now + 60},
	} {
		// 测试期间时间戳可能跳过一秒，允许 +1。
		if got := ExpireAt(tc.exp); got != tc.want && !(tc.want != 0 && got == tc.want+1) {
			t.Errorf("ExpireAt(%v) = %d, want %d", tc.exp, got, tc.want)
		}
	}
}
