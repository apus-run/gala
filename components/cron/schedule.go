package cron

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// searchYears bounds how far into the future Schedule.Next looks.
// 8 years is enough to find the next "0 0 29 2 *" even across a skipped
// leap year (2100).
// searchYears 限制 Schedule.Next 向未来搜索的距离。即使跨过被跳过的闰年
// （2100），8 年也足以找到下一个 "0 0 29 2 *"。
const searchYears = 8

// Moment represents a parsed single time moment | Moment 表示解析出的单个时间点。
type Moment struct {
	Minute    int `json:"minute"`
	Hour      int `json:"hour"`
	Day       int `json:"day"`
	Month     int `json:"month"`
	DayOfWeek int `json:"dayOfWeek"`
}

// NewMoment creates a new Moment from the specified time
// (using the wall-clock fields of t in its own location).
// NewMoment 由指定时间创建 Moment（取 t 所在时区的墙钟字段）。
func NewMoment(t time.Time) *Moment {
	return &Moment{
		Minute:    t.Minute(),
		Hour:      t.Hour(),
		Day:       t.Day(),
		Month:     int(t.Month()),
		DayOfWeek: int(t.Weekday()),
	}
}

// Schedule stores parsed information for each time component when a cron job should run.
//
// Always create it with NewSchedule. A hand-built Schedule has both
// wildcard flags unset, so its day-of-month and day-of-week fields are
// combined with OR (see IsDue).
// Schedule 保存 cron 任务应运行时各时间分量的解析结果。
//
// 请始终用 NewSchedule 创建。手工构造的 Schedule 两个通配标志均未设置，
// 其"日"与"星期"字段会按 OR 组合（见 IsDue）。
type Schedule struct {
	Minutes    map[int]struct{} `json:"minutes"`
	Hours      map[int]struct{} `json:"hours"`
	Days       map[int]struct{} `json:"days"`
	Months     map[int]struct{} `json:"months"`
	DaysOfWeek map[int]struct{} `json:"daysOfWeek"`

	// DaysWildcard and DaysOfWeekWildcard record whether the
	// day-of-month / day-of-week field started with "*" (or was "?").
	// They decide how the two day fields are combined, see IsDue.
	// DaysWildcard 和 DaysOfWeekWildcard 记录"日"/"星期"字段是否以 "*" 开头
	// （或为 "?"）。二者决定这两个字段如何组合，见 IsDue。
	DaysWildcard       bool `json:"daysWildcard"`
	DaysOfWeekWildcard bool `json:"daysOfWeekWildcard"`

	// Location is set when the expression has a "CRON_TZ=" (or "TZ=")
	// prefix. When nil, the schedule follows the timezone of its Cron.
	// Location 在表达式带 "CRON_TZ="（或 "TZ="）前缀时设置。为 nil 时，
	// 调度跟随其所属 Cron 的时区。
	Location *time.Location `json:"-"`

	expr string
}

// Expression returns the (trimmed) expression the Schedule was parsed from.
// Expression 返回解析出该 Schedule 的（去除首尾空格的）表达式。
func (s *Schedule) Expression() string {
	if s == nil {
		return ""
	}
	return s.expr
}

// IsDue checks whether the provided Moment satisfies the current Schedule.
//
// Day matching follows the classic (Vixie) cron rule: if either the
// day-of-month or the day-of-week field is a wildcard, both must match
// (AND, the wildcard side matches everything); if both are restricted, a
// match on EITHER one is enough (OR). So "0 0 1 * 1" runs on the 1st of
// every month AND on every Monday.
// IsDue 检查给定 Moment 是否满足当前 Schedule。
//
// 日期匹配遵循经典（Vixie）cron 规则："日"或"星期"任一字段为通配时，两者都须
// 匹配（AND，通配侧匹配一切）；两者都受限时，任一匹配即可（OR）。因此
// "0 0 1 * 1" 在每月 1 号和每个周一都会运行。
func (s *Schedule) IsDue(m *Moment) bool {
	if s == nil || m == nil {
		return false
	}

	return has(s.Minutes, m.Minute) &&
		has(s.Hours, m.Hour) &&
		has(s.Months, m.Month) &&
		s.dayMatches(m.Day, m.DayOfWeek)
}

func (s *Schedule) dayMatches(day, weekday int) bool {
	dom := has(s.Days, day)
	dow := has(s.DaysOfWeek, weekday)

	if s.DaysWildcard || s.DaysOfWeekWildcard {
		return dom && dow
	}

	return dom || dow
}

// Next returns the first matching minute strictly after the provided time,
// or the zero Time if there is none in the next few years
// (eg. "0 0 30 2 *").
//
// The result is expressed in s.Location when set, otherwise in the
// location of the provided time.
//
// DST handling: wall-clock minutes that don't exist (spring forward) are
// skipped, and wall-clock minutes that occur twice (fall back) are reported
// only once, at their first occurrence.
// Next 返回严格晚于给定时间的第一个匹配分钟；若未来几年内都没有（如
// "0 0 30 2 *"），返回零值 Time。
//
// 结果在设置了 s.Location 时用该时区表达，否则用给定时间的时区。
//
// DST 处理：不存在的墙钟分钟（前跳）被跳过，出现两次的墙钟分钟（回拨）
// 只在其第一次出现时报告。
func (s *Schedule) Next(after time.Time) time.Time {
	if s == nil {
		return time.Time{}
	}

	loc := after.Location()
	if s.Location != nil {
		loc = s.Location
		after = after.In(loc)
	}

	limit := after.Year() + searchYears

	// Search in "wall-clock space": a calendar without time zones, where
	// every day has 24 hours. Only the final candidate is mapped to a real
	// instant, which keeps the search itself free from DST surprises.
	// 在"墙钟空间"搜索：一个没有时区、每天 24 小时的日历。只有最终候选才映射
	// 回真实时刻，使搜索本身不受 DST 干扰。
	w := time.Date(after.Year(), after.Month(), after.Day(), after.Hour(), after.Minute()+1, 0, 0, time.UTC)

	for {
		w = s.nextWall(w, limit)
		if w.IsZero() {
			return time.Time{}
		}

		t := time.Date(w.Year(), w.Month(), w.Day(), w.Hour(), w.Minute(), 0, 0, loc)

		// t differs from w when w falls into a DST gap;
		// t is not after `after` when w is the repeated pass of a fall-back hour.
		// w 落在 DST 空档时 t 与 w 不同；w 是回拨小时的第二次经过时 t 不会晚于 after。
		if t.Hour() == w.Hour() && t.Minute() == w.Minute() && t.After(after) {
			return t
		}

		w = w.Add(time.Minute)
	}
}

// nextWall returns the first wall-clock minute >= w (w must be in UTC and
// have zero seconds) that satisfies the schedule, or the zero Time if
// it is past the limit year.
// nextWall 返回满足调度且 >= w 的第一个墙钟分钟（w 必须是 UTC 且秒为零），
// 超过年份上限则返回零值 Time。
func (s *Schedule) nextWall(w time.Time, limitYear int) time.Time {
	for w.Year() <= limitYear {
		switch {
		case !has(s.Months, int(w.Month())):
			w = time.Date(w.Year(), w.Month()+1, 1, 0, 0, 0, 0, time.UTC)
		case !s.dayMatches(w.Day(), int(w.Weekday())):
			w = time.Date(w.Year(), w.Month(), w.Day()+1, 0, 0, 0, 0, time.UTC)
		case !has(s.Hours, w.Hour()):
			w = time.Date(w.Year(), w.Month(), w.Day(), w.Hour()+1, 0, 0, 0, time.UTC)
		case !has(s.Minutes, w.Minute()):
			w = w.Add(time.Minute)
		default:
			return w
		}
	}

	return time.Time{}
}

func has(set map[int]struct{}, v int) bool {
	_, ok := set[v]
	return ok
}

var macros = map[string]string{
	"@yearly":   "0 0 1 1 *",
	"@annually": "0 0 1 1 *",
	"@monthly":  "0 0 1 * *",
	"@weekly":   "0 0 * * 0",
	"@daily":    "0 0 * * *",
	"@midnight": "0 0 * * *",
	"@hourly":   "0 * * * *",
}

var monthNames = map[string]int{
	"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6,
	"jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12,
}

var weekdayNames = map[string]int{
	"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6,
}

// fieldSpec describes the bounds and the accepted syntax of a cron field.
// fieldSpec 描述一个 cron 字段的取值范围与可接受的语法。
type fieldSpec struct {
	name          string
	min, max      int
	names         map[string]int
	allowQuestion bool // accept "?" as an alias of "*" | 接受 "?" 作为 "*" 的别名。
}

var (
	minuteSpec = fieldSpec{name: "minute", min: 0, max: 59}
	hourSpec   = fieldSpec{name: "hour", min: 0, max: 23}
	daySpec    = fieldSpec{name: "day-of-month", min: 1, max: 31, allowQuestion: true}
	monthSpec  = fieldSpec{name: "month", min: 1, max: 12, names: monthNames}
	// 7 is accepted as an alias of 0 (Sunday) and normalized by NewSchedule.
	// 7 被接受为 0（周日）的别名，由 NewSchedule 归一化。
	weekdaySpec = fieldSpec{name: "day-of-week", min: 0, max: 7, names: weekdayNames, allowQuestion: true}
)

// NewSchedule creates a new Schedule from a cron expression.
//
// A cron expression could be a macro OR 5 fields separated by whitespace,
// representing: minute, hour, day of the month, month and day of the week.
// Supported field formats: wildcard "*" (or "?" for the day fields); a
// value like 5, or a name for the month (JAN-DEC) and day of week
// (SUN-SAT); a range like 1-30 or MON-FRI; a step like */n, 1-30/n or 5/n
// (shorthand for 5-max/n); a list like 1,2,3,10-20/n,MON. Day of week
// accepts 0-7, where both 0 and 7 mean Sunday.
//
// The following macros are supported (case insensitive): @yearly (or
// @annually), @monthly, @weekly, @daily (or @midnight), @hourly.
//
// The expression may be prefixed with "CRON_TZ=<IANA name> " (or "TZ=")
// to pin the schedule to a specific timezone, eg.
// "CRON_TZ=Asia/Shanghai 0 9 * * MON-FRI". Loading a named timezone needs
// the system tz database; programs that may run without one should import
// the embedded copy: import _ "time/tzdata".
//
// If both the day-of-month and day-of-week fields are restricted, the
// schedule runs when EITHER of them matches (see Schedule.IsDue).
// NewSchedule 从 cron 表达式创建 Schedule。
//
// cron 表达式可以是宏，或由空白分隔的 5 个字段：分钟、小时、日、月、星期。
// 支持的字段格式：通配 "*"（日字段还可用 "?"）；单个值如 5，月份（JAN-DEC）
// 与星期（SUN-SAT）还可用名称；范围如 1-30、MON-FRI；步进如 */n、1-30/n、
// 5/n（即 5-max/n 的简写）；列表如 1,2,3,10-20/n,MON。星期字段接受 0-7，
// 0 和 7 都表示周日。
//
// 支持的宏（不区分大小写）：@yearly（或 @annually）、@monthly、@weekly、
// @daily（或 @midnight）、@hourly。
//
// 表达式可带 "CRON_TZ=<IANA 名称> "（或 "TZ="）前缀，把调度钉在特定时区，
// 如 "CRON_TZ=Asia/Shanghai 0 9 * * MON-FRI"。加载命名时区需要系统 tz
// 数据库；可能在没有数据库的环境运行的程序应导入内嵌副本：
// import _ "time/tzdata"。
//
// 若"日"与"星期"字段都受限，任一匹配即运行（见 Schedule.IsDue）。
func NewSchedule(cronExpr string) (*Schedule, error) {
	expr := strings.TrimSpace(cronExpr)

	var loc *time.Location
	if strings.HasPrefix(expr, "CRON_TZ=") || strings.HasPrefix(expr, "TZ=") {
		end := strings.IndexFunc(expr, unicode.IsSpace)
		if end < 0 {
			return nil, errors.New("invalid cron expression - missing schedule after the timezone prefix")
		}

		name := expr[strings.Index(expr, "=")+1 : end]
		if name == "" {
			return nil, errors.New("invalid cron expression - empty timezone name")
		}

		var err error
		loc, err = time.LoadLocation(name)
		if err != nil {
			return nil, fmt.Errorf("invalid cron expression - unknown timezone %q: %w", name, err)
		}

		expr = strings.TrimSpace(expr[end:])
	}

	if v, ok := macros[strings.ToLower(expr)]; ok {
		expr = v
	}

	segments := strings.Fields(expr)
	if len(segments) != 5 {
		return nil, errors.New("invalid cron expression - must be a valid macro or to have exactly 5 whitespace separated segments")
	}

	minutes, _, err := parseCronSegment(segments[0], minuteSpec)
	if err != nil {
		return nil, err
	}

	hours, _, err := parseCronSegment(segments[1], hourSpec)
	if err != nil {
		return nil, err
	}

	days, daysWildcard, err := parseCronSegment(segments[2], daySpec)
	if err != nil {
		return nil, err
	}

	months, _, err := parseCronSegment(segments[3], monthSpec)
	if err != nil {
		return nil, err
	}

	daysOfWeek, dowWildcard, err := parseCronSegment(segments[4], weekdaySpec)
	if err != nil {
		return nil, err
	}

	// 7 is Sunday too
	// 7 也表示周日。
	if _, ok := daysOfWeek[7]; ok {
		delete(daysOfWeek, 7)
		daysOfWeek[0] = struct{}{}
	}

	return &Schedule{
		Minutes:            minutes,
		Hours:              hours,
		Days:               days,
		Months:             months,
		DaysOfWeek:         daysOfWeek,
		DaysWildcard:       daysWildcard,
		DaysOfWeekWildcard: dowWildcard,
		Location:           loc,
		expr:               strings.TrimSpace(cronExpr),
	}, nil
}

// parseCronSegment parses a single cron expression segment and returns its
// time schedule slots and whether it contains a wildcard.
// parseCronSegment 解析单个 cron 表达式字段，返回其时间槽位集合及是否包含通配。
func parseCronSegment(segment string, f fieldSpec) (map[int]struct{}, bool, error) {
	slots := map[int]struct{}{}
	wildcard := false

	for _, part := range strings.Split(segment, ",") {
		lo, hi, step, isWildcard, err := parseCronTerm(part, f)
		if err != nil {
			return nil, false, fmt.Errorf("invalid %s segment %q: %w", f.name, segment, err)
		}

		wildcard = wildcard || isWildcard

		for i := lo; i <= hi; i += step {
			slots[i] = struct{}{}
		}
	}

	return slots, wildcard, nil
}

// parseCronTerm parses a single list item: "*", "5", "1-30", "*/n", "1-30/n" or "5/n".
// parseCronTerm 解析单个列表项："*"、"5"、"1-30"、"*/n"、"1-30/n" 或 "5/n"。
func parseCronTerm(term string, f fieldSpec) (lo, hi, step int, wildcard bool, err error) {
	rangePart, stepPart, hasStep := strings.Cut(term, "/")

	step = 1
	if hasStep {
		step, err = parseNumber(stepPart)
		if err != nil {
			return 0, 0, 0, false, fmt.Errorf("invalid step %q", stepPart)
		}
		if size := f.max - f.min + 1; step < 1 || step > size {
			return 0, 0, 0, false, fmt.Errorf("step must be between 1 and %d", size)
		}
	}

	switch {
	case rangePart == "*":
		return f.min, f.max, step, true, nil

	case rangePart == "?" && f.allowQuestion:
		if hasStep {
			return 0, 0, 0, false, errors.New("\"?\" can't be used with a step")
		}
		return f.min, f.max, 1, true, nil
	}

	loStr, hiStr, isRange := strings.Cut(rangePart, "-")

	lo, err = f.parseValue(loStr)
	if err != nil {
		return 0, 0, 0, false, err
	}

	switch {
	case isRange:
		hi, err = f.parseValue(hiStr)
		if err != nil {
			return 0, 0, 0, false, err
		}
		if hi < lo {
			return 0, 0, 0, false, fmt.Errorf("range start %d is greater than its end %d", lo, hi)
		}
	case hasStep:
		hi = f.max // "5/15" == "5-max/15" | "5/15" 即 "5-max/15"。
	default:
		hi = lo
	}

	return lo, hi, step, false, nil
}

func (f fieldSpec) parseValue(s string) (int, error) {
	if s == "" {
		return 0, errors.New("empty value")
	}

	n, ok := f.names[strings.ToLower(s)]
	if !ok {
		var err error
		n, err = parseNumber(s)
		if err != nil {
			return 0, fmt.Errorf("invalid value %q", s)
		}
	}

	if n < f.min || n > f.max {
		return 0, fmt.Errorf("value %d is out of the allowed range %d-%d", n, f.min, f.max)
	}

	return n, nil
}

// parseNumber parses a plain unsigned decimal number
// (unlike strconv.Atoi it rejects a leading "+" or "-").
// parseNumber 解析无符号十进制整数（与 strconv.Atoi 不同，拒绝前导 "+" 或 "-"）。
func parseNumber(s string) (int, error) {
	n, err := strconv.ParseUint(s, 10, 16)
	if err != nil {
		return 0, err
	}
	return int(n), nil
}
