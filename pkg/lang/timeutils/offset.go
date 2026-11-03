package timeutils

import (
	"time"

	"github.com/tkuchiki/go-timezone"
)

var tzInfo = timezone.New()

// TimeToOffset calculates returns the time offset for current time zone.
// TimeToOffset 计算并返回 t 所在时区的 UTC 偏移秒数。
func TimeToOffset(t time.Time) int64 {
	tz, _ := t.Zone()
	infos, _ := tzInfo.GetTzAbbreviationInfo(tz)
	if len(infos) == 0 {
		return 0
	}

	// We can tolerate ambiguities, but only if all timezones have the same offset.
	// 缩写有歧义时可以容忍，但仅当所有候选时区的偏移一致。
	offset := infos[0].Offset()
	for _, info := range infos[1:] {
		if info.Offset() != offset {
			return 0
		}
	}

	return int64(offset)
}
