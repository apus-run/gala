package cron

import (
	"math/rand"
	"reflect"
	"sort"
	"testing"
	"time"

	_ "time/tzdata"
)

func keys(m map[int]struct{}) []int {
	r := make([]int, 0, len(m))
	for k := range m {
		r = append(r, k)
	}
	sort.Ints(r)
	return r
}

func mustLoc(t testing.TB, name string) *time.Location {
	t.Helper()
	l, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestNewScheduleParsing(t *testing.T) {
	t.Parallel()

	type want struct {
		minutes, hours, days, months, dows []int
		daysStar, dowStar                  bool
	}

	seq := func(from, to, step int) []int {
		var r []int
		for i := from; i <= to; i += step {
			r = append(r, i)
		}
		return r
	}

	scenarios := []struct {
		expr string
		want want
	}{
		{"* * * * *", want{seq(0, 59, 1), seq(0, 23, 1), seq(1, 31, 1), seq(1, 12, 1), seq(0, 6, 1), true, true}},
		{"  */15\t2-4  ?  jan,MAR  Mon-Fri ", want{[]int{0, 15, 30, 45}, []int{2, 3, 4}, seq(1, 31, 1), []int{1, 3}, []int{1, 2, 3, 4, 5}, true, false}},
		{"5/20 0 1,15 * 7", want{[]int{5, 25, 45}, []int{0}, []int{1, 15}, seq(1, 12, 1), []int{0}, false, false}},
		{"0 0 * * 5-7", want{[]int{0}, []int{0}, seq(1, 31, 1), seq(1, 12, 1), []int{0, 5, 6}, true, false}},
		{"0 0 * * */2", want{[]int{0}, []int{0}, seq(1, 31, 1), seq(1, 12, 1), []int{0, 2, 4, 6}, true, true}},
		{"@DAILY", want{[]int{0}, []int{0}, seq(1, 31, 1), seq(1, 12, 1), seq(0, 6, 1), true, true}},
		{"@weekly", want{[]int{0}, []int{0}, seq(1, 31, 1), seq(1, 12, 1), []int{0}, true, false}},
	}

	for _, s := range scenarios {
		t.Run(s.expr, func(t *testing.T) {
			sc, err := NewSchedule(s.expr)
			if err != nil {
				t.Fatal(err)
			}

			got := want{keys(sc.Minutes), keys(sc.Hours), keys(sc.Days), keys(sc.Months), keys(sc.DaysOfWeek), sc.DaysWildcard, sc.DaysOfWeekWildcard}
			if !reflect.DeepEqual(got, s.want) {
				t.Fatalf("\nwant %+v\ngot  %+v", s.want, got)
			}
		})
	}
}

func TestNewScheduleInvalid(t *testing.T) {
	t.Parallel()

	for _, expr := range []string{
		"", "* * * *", "* * * * * *", "@every 5m", "@nope",
		"60 * * * *", "* 24 * * *", "* * 0 * *", "* * 32 * *", "* * * 0 *", "* * * 13 *", "* * * * 8",
		"-1 * * * *", "+1 * * * *", "1- * * * *", "-5 * * * *", "1-2-3 * * * *",
		"30-10 * * * *", "1,,2 * * * *", ",1 * * * *", "1, * * * *",
		"*/0 * * * *", "*/61 * * * *", "*/a * * * *", "*/-1 * * * *", "*/2/3 * * * *",
		"?/2 * * * *", "? * * * *", "a * * * *", "* * * foo *", "* * * * funday",
		"CRON_TZ=Nope/Nowhere * * * * *", "CRON_TZ= * * * * *", "CRON_TZ=UTC", "999999999999 * * * *",
	} {
		if _, err := NewSchedule(expr); err == nil {
			t.Errorf("expected error for %q", expr)
		}
	}
}

func TestScheduleIsDueDayRules(t *testing.T) {
	t.Parallel()

	// 2026-06-01 is a Monday, 2026-06-08 is a Monday, 2026-06-02 is a Tuesday,
	// 2026-06-15 is a Monday, 2026-07-01 is a Wednesday
	scenarios := []struct {
		expr string
		date string
		want bool
	}{
		// both restricted -> OR
		{"0 0 1 * 1", "2026-06-01", true},  // 1st and Monday
		{"0 0 1 * 1", "2026-06-08", true},  // Monday only
		{"0 0 1 * 1", "2026-07-01", true},  // 1st only
		{"0 0 1 * 1", "2026-06-02", false}, // neither
		// one wildcard -> AND
		{"0 0 * * 1", "2026-06-08", true},
		{"0 0 * * 1", "2026-06-02", false},
		{"0 0 15 * *", "2026-06-15", true},
		{"0 0 15 * *", "2026-06-08", false},
		{"0 0 15 * ?", "2026-06-15", true},
		{"0 0 ? * 1", "2026-06-02", false},
		// a day field that starts with "*" counts as a wildcard -> AND ("*/2" means 1,3,5,...,31)
		{"0 0 */2 * 1", "2026-06-08", false}, // Monday, but 8 is not in */2
		{"0 0 */2 * 1", "2026-06-15", true},  // Monday and 15 is in */2
		{"0 0 */2 * 1", "2026-06-02", false}, // Tuesday
	}

	for _, s := range scenarios {
		d, err := time.Parse("2006-01-02", s.date)
		if err != nil {
			t.Fatal(err)
		}

		sc, err := NewSchedule(s.expr)
		if err != nil {
			t.Fatal(err)
		}

		if got := sc.IsDue(NewMoment(d)); got != s.want {
			t.Errorf("%q on %s: want %v, got %v", s.expr, s.date, s.want, got)
		}
	}
}

func TestScheduleIsDueNilSafe(t *testing.T) {
	t.Parallel()

	var s *Schedule
	if s.IsDue(&Moment{}) {
		t.Fatal("nil schedule must not be due")
	}

	sc, _ := NewSchedule("* * * * *")
	if sc.IsDue(nil) {
		t.Fatal("nil moment must not be due")
	}
}

func TestScheduleExpression(t *testing.T) {
	t.Parallel()

	sc, err := NewSchedule("  @daily ")
	if err != nil {
		t.Fatal(err)
	}
	if sc.Expression() != "@daily" {
		t.Fatalf("got %q", sc.Expression())
	}
}

func TestScheduleTimezonePrefix(t *testing.T) {
	t.Parallel()

	for _, expr := range []string{"CRON_TZ=Asia/Shanghai 0 9 * * *", "TZ=Asia/Shanghai   @daily"} {
		sc, err := NewSchedule(expr)
		if err != nil {
			t.Fatal(expr, err)
		}
		if sc.Location == nil || sc.Location.String() != "Asia/Shanghai" {
			t.Fatalf("%q: unexpected location %v", expr, sc.Location)
		}
	}

	sc, _ := NewSchedule("CRON_TZ=Asia/Shanghai 0 9 * * *")
	after := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC) // 08:00 in Shanghai
	got := sc.Next(after)
	want := time.Date(2026, 6, 1, 9, 0, 0, 0, mustLoc(t, "Asia/Shanghai"))
	if !got.Equal(want) || got.Location().String() != "Asia/Shanghai" {
		t.Fatalf("want %v, got %v", want, got)
	}
}

func TestScheduleNext(t *testing.T) {
	t.Parallel()

	utc := time.UTC
	ny := mustLoc(t, "America/New_York")

	scenarios := []struct {
		name  string
		expr  string
		after time.Time
		want  time.Time // zero means "never"
	}{
		{"every minute", "* * * * *", time.Date(2026, 6, 1, 10, 0, 0, 0, utc), time.Date(2026, 6, 1, 10, 1, 0, 0, utc)},
		{"seconds are dropped", "* * * * *", time.Date(2026, 6, 1, 10, 0, 59, 999, utc), time.Date(2026, 6, 1, 10, 1, 0, 0, utc)},
		{"strictly after", "30 10 * * *", time.Date(2026, 6, 1, 10, 30, 0, 0, utc), time.Date(2026, 6, 2, 10, 30, 0, 0, utc)},
		{"same day", "30 10 * * *", time.Date(2026, 6, 1, 10, 29, 59, 0, utc), time.Date(2026, 6, 1, 10, 30, 0, 0, utc)},
		{"hour rollover", "5 * * * *", time.Date(2026, 6, 1, 10, 6, 0, 0, utc), time.Date(2026, 6, 1, 11, 5, 0, 0, utc)},
		{"year rollover", "0 0 1 1 *", time.Date(2026, 6, 1, 10, 0, 0, 0, utc), time.Date(2027, 1, 1, 0, 0, 0, 0, utc)},
		{"month length", "0 0 31 * *", time.Date(2026, 4, 1, 0, 0, 0, 0, utc), time.Date(2026, 5, 31, 0, 0, 0, 0, utc)},
		{"leap day", "0 0 29 2 *", time.Date(2026, 3, 1, 0, 0, 0, 0, utc), time.Date(2028, 2, 29, 0, 0, 0, 0, utc)},
		{"leap day over 2100", "0 0 29 2 *", time.Date(2097, 3, 1, 0, 0, 0, 0, utc), time.Date(2104, 2, 29, 0, 0, 0, 0, utc)},
		{"never", "0 0 30 2 *", time.Date(2026, 1, 1, 0, 0, 0, 0, utc), time.Time{}},
		{"weekday", "0 9 * * MON-FRI", time.Date(2026, 6, 5, 9, 0, 0, 0, utc), time.Date(2026, 6, 8, 9, 0, 0, 0, utc)}, // Fri -> Mon
		{"day OR weekday", "0 0 1 * 1", time.Date(2026, 6, 2, 0, 0, 0, 0, utc), time.Date(2026, 6, 8, 0, 0, 0, 0, utc)},
		{"day OR weekday (day first)", "0 0 1 * 1", time.Date(2026, 6, 29, 0, 0, 0, 0, utc), time.Date(2026, 7, 1, 0, 0, 0, 0, utc)},
		{"step", "*/20 */6 * * *", time.Date(2026, 6, 1, 6, 40, 0, 0, utc), time.Date(2026, 6, 1, 12, 0, 0, 0, utc)},

		// America/New_York: 2026-03-08 02:00 EST -> 03:00 EDT, 2026-11-01 02:00 EDT -> 01:00 EST
		{"DST spring: skipped minute", "30 2 * * *", time.Date(2026, 3, 7, 12, 0, 0, 0, ny), time.Date(2026, 3, 9, 2, 30, 0, 0, ny)},
		{"DST spring: hourly", "0 * * * *", time.Date(2026, 3, 8, 1, 30, 0, 0, ny), time.Date(2026, 3, 8, 3, 0, 0, 0, ny)},
		// fall back happens at 06:00 UTC: 01:30 EDT = 05:30 UTC (first pass), 01:30 EST = 06:30 UTC (second pass)
		{"DST fall: first pass only", "30 1 * * *", time.Date(2026, 11, 1, 5, 0, 0, 0, utc).In(ny), time.Date(2026, 11, 1, 5, 30, 0, 0, utc)},
		{"DST fall: not again after first pass", "30 1 * * *", time.Date(2026, 11, 1, 5, 30, 0, 0, utc).In(ny), time.Date(2026, 11, 2, 1, 30, 0, 0, ny)},
		{"DST fall: not again in second pass", "30 1 * * *", time.Date(2026, 11, 1, 6, 10, 0, 0, utc).In(ny), time.Date(2026, 11, 2, 1, 30, 0, 0, ny)},
	}

	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			sc, err := NewSchedule(s.expr)
			if err != nil {
				t.Fatal(err)
			}

			got := sc.Next(s.after)

			if s.want.IsZero() {
				if !got.IsZero() {
					t.Fatalf("expected no next run, got %v", got)
				}
				return
			}

			if !got.Equal(s.want) {
				t.Fatalf("want %v, got %v", s.want, got)
			}
		})
	}
}

// Next must always move forward and return a time that IsDue.
func TestScheduleNextInvariants(t *testing.T) {
	t.Parallel()

	zones := []string{"UTC", "America/New_York", "Europe/London", "Asia/Kolkata", "Australia/Lord_Howe", "Pacific/Apia", "America/Sao_Paulo"}
	exprs := []string{"* * * * *", "*/7 * * * *", "30 1 * * *", "0 2 * * *", "0 0 * * *", "15 3 1 * 1", "0 */2 * * *", "59 23 31 12 *", "0 12 29 2 *"}

	rnd := rand.New(rand.NewSource(1))

	for _, z := range zones {
		loc := mustLoc(t, z)

		for _, e := range exprs {
			sc, err := NewSchedule(e)
			if err != nil {
				t.Fatal(err)
			}

			for i := 0; i < 150; i++ {
				// random instant between 2024 and 2030, with random seconds
				base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(rnd.Int63n(int64(6 * 365 * 24 * time.Hour))))
				after := base.In(loc)

				next := sc.Next(after)
				if next.IsZero() {
					continue
				}
				if !next.After(after) {
					t.Fatalf("%s %q: Next(%v) = %v is not after", z, e, after, next)
				}
				if !sc.IsDue(NewMoment(next)) {
					t.Fatalf("%s %q: Next(%v) = %v is not due", z, e, after, next)
				}
				if next.Second() != 0 || next.Nanosecond() != 0 {
					t.Fatalf("%s %q: Next(%v) = %v has sub-minute fields", z, e, after, next)
				}
			}
		}
	}
}

func BenchmarkScheduleNext(b *testing.B) {
	sc, _ := NewSchedule("0 0 29 2 *")
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	for i := 0; i < b.N; i++ {
		sc.Next(now)
	}
}
