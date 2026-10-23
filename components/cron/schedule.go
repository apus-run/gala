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
const searchYears = 8

// Moment represents a parsed single time moment.
type Moment struct {
	Minute    int `json:"minute"`
	Hour      int `json:"hour"`
	Day       int `json:"day"`
	Month     int `json:"month"`
	DayOfWeek int `json:"dayOfWeek"`
}

// NewMoment creates a new Moment from the specified time
// (using the wall-clock fields of t in its own location).
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
type Schedule struct {
	Minutes    map[int]struct{} `json:"minutes"`
	Hours      map[int]struct{} `json:"hours"`
	Days       map[int]struct{} `json:"days"`
	Months     map[int]struct{} `json:"months"`
	DaysOfWeek map[int]struct{} `json:"daysOfWeek"`

	// DaysWildcard and DaysOfWeekWildcard record whether the
	// day-of-month / day-of-week field started with "*" (or was "?").
	// They decide how the two day fields are combined, see IsDue.
	DaysWildcard       bool `json:"daysWildcard"`
	DaysOfWeekWildcard bool `json:"daysOfWeekWildcard"`

	// Location is set when the expression has a "CRON_TZ=" (or "TZ=")
	// prefix. When nil, the schedule follows the timezone of its Cron.
	Location *time.Location `json:"-"`

	expr string
}

// Expression returns the (trimmed) expression the Schedule was parsed from.
func (s *Schedule) Expression() string {
	if s == nil {
		return ""
	}
	return s.expr
}

// IsDue checks whether the provided Moment satisfies the current Schedule.
//
// Day matching follows the classic (Vixie) cron rule:
//   - if either the day-of-month or the day-of-week field is a wildcard,
//     both must match (AND - the wildcard side matches everything);
//   - if both are restricted, a match on EITHER one is enough (OR).
//
// So "0 0 1 * 1" runs on the 1st of every month AND on every Monday.
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
	w := time.Date(after.Year(), after.Month(), after.Day(), after.Hour(), after.Minute()+1, 0, 0, time.UTC)

	for {
		w = s.nextWall(w, limit)
		if w.IsZero() {
			return time.Time{}
		}

		t := time.Date(w.Year(), w.Month(), w.Day(), w.Hour(), w.Minute(), 0, 0, loc)

		// t differs from w when w falls into a DST gap;
		// t is not after `after` when w is the repeated pass of a fall-back hour.
		if t.Hour() == w.Hour() && t.Minute() == w.Minute() && t.After(after) {
			return t
		}

		w = w.Add(time.Minute)
	}
}

// nextWall returns the first wall-clock minute >= w (w must be in UTC and
// have zero seconds) that satisfies the schedule, or the zero Time if
// it is past the limit year.
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
type fieldSpec struct {
	name          string
	min, max      int
	names         map[string]int
	allowQuestion bool // accept "?" as an alias of "*"
}

var (
	minuteSpec = fieldSpec{name: "minute", min: 0, max: 59}
	hourSpec   = fieldSpec{name: "hour", min: 0, max: 23}
	daySpec    = fieldSpec{name: "day-of-month", min: 1, max: 31, allowQuestion: true}
	monthSpec  = fieldSpec{name: "month", min: 1, max: 12, names: monthNames}
	// 7 is accepted as an alias of 0 (Sunday) and normalized by NewSchedule.
	weekdaySpec = fieldSpec{name: "day-of-week", min: 0, max: 7, names: weekdayNames, allowQuestion: true}
)

// NewSchedule creates a new Schedule from a cron expression.
//
// A cron expression could be a macro OR 5 fields separated by whitespace,
// representing: minute, hour, day of the month, month and day of the week.
//
// The following field formats are supported:
//   - wildcard: * (or ? for the day fields)
//   - value:    5, or a name for the month (JAN-DEC) and day of week (SUN-SAT)
//   - range:    1-30, MON-FRI
//   - step:     */n, 1-30/n or 5/n (shorthand for 5-max/n)
//   - list:     1,2,3,10-20/n,MON
//
// Day of week accepts 0-7, where both 0 and 7 mean Sunday.
//
// The following macros are supported (case insensitive):
//   - @yearly (or @annually)
//   - @monthly
//   - @weekly
//   - @daily (or @midnight)
//   - @hourly
//
// The expression may be prefixed with "CRON_TZ=<IANA name> " (or "TZ=")
// to pin the schedule to a specific timezone, eg.
// "CRON_TZ=Asia/Shanghai 0 9 * * MON-FRI". Loading a named timezone needs
// the system tz database; programs that may run without one should import
// the embedded copy: import _ "time/tzdata".
//
// If both the day-of-month and day-of-week fields are restricted, the
// schedule runs when EITHER of them matches (see Schedule.IsDue).
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
		hi = f.max // "5/15" == "5-max/15"
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
func parseNumber(s string) (int, error) {
	n, err := strconv.ParseUint(s, 10, 16)
	if err != nil {
		return 0, err
	}
	return int(n), nil
}
