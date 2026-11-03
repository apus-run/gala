package timeutils

import (
	"time"

	"github.com/apus-run/gala/pkg/lang/must"
)

// MustParse parses the given value into a `time.Time` according to the layout, or panics if there is a parse error.
// MustParse 按 layout 将给定值解析为 `time.Time`，解析出错时 panic。
func MustParse(layout string, value string) time.Time {
	ts, err := time.Parse(layout, value)
	must.CrashOnError(err)
	return ts
}
