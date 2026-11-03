// Package timeutils provides calendar date arithmetic (second-granularity
// Date type with Add/Advance methods) and small time helpers.
// 包 timeutils 提供日历日期运算（秒精度的 Date 类型及 Add/Advance 系列方法）
// 与若干时间小工具。
package timeutils

import (
	"time"

	mathutil "github.com/apus-run/gala/pkg/lang/mathutils"
)

const (
	// MonthsInYear is the number of months in a calendar year | 一个日历年的月数
	MonthsInYear = 12
	// HoursInDay is the number of hours in a day | 一天的小时数
	HoursInDay = 24
	// MinutesInHour is the number of minutes in an hour | 一小时的分钟数
	MinutesInHour = 60
	// SecondsInMinute is the number of seconds in a minute | 一分钟的秒数
	SecondsInMinute = 60

	// DaysInWeek is the number of days in a week | 一周的天数
	DaysInWeek = 7
)

var maxDaysInMonth = [...]int{
	31,
	29,
	31,
	30,
	31,
	30,
	31,
	31,
	30,
	31,
	30,
	31,
}

// DaysInMonth returns the number of days in the given month and year.
// DaysInMonth 返回给定年份与月份的天数。
func DaysInMonth(month int, year int) int {
	if month == int(time.February) && !IsLeapYear(year) {
		return 28
	}
	return maxDaysInMonth[month-1]
}

// MaxDaysInMonth returns the number of days in the given month in *any* year.
// MaxDaysInMonth 返回给定月份在任意年份中的最大天数。
func MaxDaysInMonth(month int) int {
	return maxDaysInMonth[month-1]
}

// IsLeapYear returns whether the given year is a leap year.
// IsLeapYear 判断给定年份是否为闰年。
func IsLeapYear(year int) bool {
	return year%4 == 0 && (year%100 != 0 || year%400 == 0)
}

// TimeDiffDays returns the duration t1 - t2 in days
// TimeDiffDays 返回 t1 - t2 的时长，单位为天。
func TimeDiffDays(t1 time.Time, t2 time.Time) int {
	hours := int(t1.Sub(t2).Hours())
	return hours / HoursInDay
}

// Date is a representation of a time point (in some time zone) with second
// granularity.
// Date 表示秒精度的某一时刻（处于某个时区）。
type Date struct {
	Year, Month, Day, Hour, Minute, Second int
}

func (d *Date) clampDayOfMonth() {
	if d.Day > DaysInMonth(d.Month, d.Year) {
		d.Day = DaysInMonth(d.Month, d.Year)
	}
}

// AddYear adds the given value to the calendar year of this date. The day of month will be clamped to the maximum
// number of days in the resulting month/year combination (regardless of whether num is negative or positive).
// AddYear 将给定数值加到本日期的日历年上。日字段会被钳制到结果月/年组合的最大天数
// （无论 num 为正还是为负）。
func (d *Date) AddYear(num int) {
	d.Year += num
	d.clampDayOfMonth()
}

// AddMonth adds the given value to the calendar month of this date. The day of month will be clamped to the maximum
// number of days in the resulting month/year combination (regardless of whether num is negative or positive).
// AddMonth 将给定数值加到本日期的日历月上，必要时进/退位年份。日字段会被钳制到结果
// 月/年组合的最大天数（无论 num 为正还是为负）。
func (d *Date) AddMonth(num int) {
	newMonth := d.Month + num
	if newMonth < 1 {
		d.Year -= (-newMonth)/MonthsInYear + 1
	} else {
		d.Year += (newMonth - 1) / MonthsInYear
	}
	d.Month = mathutil.Mod(newMonth-1, MonthsInYear) + 1
	d.clampDayOfMonth()
}

// AddDay adds the given value to the calendar day of this date.
// AddDay 将给定数值加到本日期的日字段上，必要时进/退位月与年。
func (d *Date) AddDay(num int) {
	newDay := d.Day + num
	for newDay < 1 {
		d.Month--
		if d.Month < 1 {
			d.Month = 12
			d.Year--
		}
		newDay += DaysInMonth(d.Month, d.Year)
	}
	for newDay > DaysInMonth(d.Month, d.Year) {
		newDay -= DaysInMonth(d.Month, d.Year)
		d.Month++
		if d.Month > 12 {
			d.Month = 1
			d.Year++
		}
	}
	d.Day = newDay
}

// AddHour adds the given value to the hour value of this date.
// AddHour 将给定数值加到本日期的小时字段上，必要时进/退位天。
func (d *Date) AddHour(num int) {
	newHour := d.Hour + num
	if newHour < 0 {
		d.AddDay(-((-newHour-1)/HoursInDay + 1))
	} else if newHour >= HoursInDay {
		d.AddDay(newHour / HoursInDay)
	}
	newHour = mathutil.Mod(newHour, HoursInDay)
	d.Hour = newHour
}

// AddMinute adds the given value to the minute value of this date.
// AddMinute 将给定数值加到本日期的分钟字段上，必要时进/退位小时。
func (d *Date) AddMinute(num int) {
	newMinute := d.Minute + num
	if newMinute < 0 {
		d.AddHour(-((-newMinute-1)/MinutesInHour + 1))
	} else if newMinute >= MinutesInHour {
		d.AddHour(newMinute / MinutesInHour)
	}
	newMinute = mathutil.Mod(newMinute, MinutesInHour)
	d.Minute = newMinute
}

// AddSecond adds the given value to the second value of this date.
// AddSecond 将给定数值加到本日期的秒字段上，必要时进/退位分钟。
func (d *Date) AddSecond(num int) {
	newSecond := d.Second + num
	if newSecond < 0 {
		d.AddMinute(-((-newSecond-1)/SecondsInMinute + 1))
	} else if newSecond >= SecondsInMinute {
		d.AddMinute(newSecond / SecondsInMinute)
	}
	newSecond = mathutil.Mod(newSecond, SecondsInMinute)
	d.Second = newSecond
}

// TimeMonth returns the calendar month as a `time.Month` value.
// TimeMonth 以 `time.Month` 类型返回本日期的日历月。
func (d Date) TimeMonth() time.Month {
	return time.Month(d.Month)
}

// ToTime returns the `time.Time` representation of this date in the given timezone.
// ToTime 返回本日期在给定时区下的 `time.Time` 表示。
func (d Date) ToTime(loc *time.Location) time.Time {
	return time.Date(d.Year, d.TimeMonth(), d.Day, d.Hour, d.Minute, d.Second, 0, loc)
}

// WeekdayOfMonth returns the "weekday of month" of the day represented by this date.
// WeekdayOfMonth 返回本日期对应的“月内星期”值。
func (d Date) WeekdayOfMonth() int {
	t := d.ToTime(time.UTC)
	return int(t.Weekday()) + ((d.Day-1)/DaysInWeek)*DaysInWeek
}

// DateFromTime converts a `time.Time` to a `Date` representation.
// DateFromTime 将 `time.Time` 转换为 `Date` 表示。
func DateFromTime(t time.Time) Date {
	return Date{
		Year:   t.Year(),
		Month:  int(t.Month()),
		Day:    t.Day(),
		Hour:   t.Hour(),
		Minute: t.Minute(),
		Second: t.Second(),
	}
}

// AdvanceMonth advances this date to the next calendar date where month has the given value (wrapping the year around,
// if necessary).
// AdvanceMonth 将本日期推进到月份等于给定值的下一个日历日期（必要时跨年）。
func (d *Date) AdvanceMonth(newMonth int) {
	if newMonth < d.Month {
		d.Year++
	}
	if newMonth != d.Month {
		d.Day = 1
		d.Hour = 0
		d.Minute = 0
		d.Second = 0
	}
	d.Month = newMonth
}

// AdvanceDay advances this date to the next calendar date where day has the given value (wrapping the month around,
// if necessary).
// AdvanceDay 将本日期推进到日字段等于给定值的下一个日历日期（必要时跨月）。
func (d *Date) AdvanceDay(newDay int) {
	if newDay < d.Day {
		d.AddMonth(1)
		for newDay > DaysInMonth(d.Month, d.Year) {
			d.AddMonth(1)
		}
	}
	if newDay != d.Day {
		d.Hour = 0
		d.Minute = 0
		d.Second = 0
	}
	d.Day = newDay
}

// AdvanceHour advances this date to the next calendar date where hour has the given value (wrapping the day around,
// if necessary).
// AdvanceHour 将本日期推进到小时字段等于给定值的下一个日历日期（必要时跨天）。
func (d *Date) AdvanceHour(newHour int) {
	if newHour < d.Hour {
		d.AddDay(1)
	}
	if newHour != d.Hour {
		d.Minute = 0
		d.Second = 0
	}
	d.Hour = newHour
}

// AdvanceMinute advances this date to the next calendar date where minute has the given value (wrapping the hour around,
// if necessary).
// AdvanceMinute 将本日期推进到分钟字段等于给定值的下一个日历日期（必要时跨小时）。
func (d *Date) AdvanceMinute(newMinute int) {
	if newMinute < d.Minute {
		d.AddHour(1)
	}
	if newMinute != d.Minute {
		d.Second = 0
	}
	d.Minute = newMinute
}

// AdvanceSecond advances this date to the next calendar date where second has the given value (wrapping the minute around,
// if necessary).
// AdvanceSecond 将本日期推进到秒字段等于给定值的下一个日历日期（必要时跨分钟）。
func (d *Date) AdvanceSecond(newSecond int) {
	if newSecond < d.Second {
		d.AddMinute(1)
	}
	d.Second = newSecond
}

// AdvanceYearBy advances the calendar year by the given number. Negative numbers are not allowed and will leave this
// date unchanged.
// AdvanceYearBy 将日历年推进给定数值（并把月、日、时、分、秒清零）。不允许负数，
// 负数时本日期保持不变。
func (d *Date) AdvanceYearBy(num int) {
	if num <= 0 {
		return
	}
	d.Year += num
	d.Month = 1
	d.Day = 1
	d.Hour = 0
	d.Minute = 0
	d.Second = 0
}

// AdvanceMonthBy advances the calendar month by the given number. Negative numbers are not allowed and will leave this
// date unchanged.
// AdvanceMonthBy 将日历月推进给定数值（并把日、时、分、秒清零）。不允许负数，
// 负数时本日期保持不变。
func (d *Date) AdvanceMonthBy(num int) {
	if num <= 0 {
		return
	}
	d.AddMonth(num)
	d.Day = 1
	d.Hour = 0
	d.Minute = 0
	d.Second = 0
}

// AdvanceDayBy advances the calendar day by the given number. Negative numbers are not allowed and will leave this
// date unchanged.
// AdvanceDayBy 将日历日推进给定数值（并把时、分、秒清零）。不允许负数，
// 负数时本日期保持不变。
func (d *Date) AdvanceDayBy(num int) {
	if num <= 0 {
		return
	}
	d.AddDay(num)
	d.Hour = 0
	d.Minute = 0
	d.Second = 0
}

// AdvanceHourBy advances the hour of day by the given number. Negative numbers are not allowed and will leave this
// date unchanged.
// AdvanceHourBy 将一天中的小时推进给定数值（并把分、秒清零）。不允许负数，
// 负数时本日期保持不变。
func (d *Date) AdvanceHourBy(num int) {
	if num <= 0 {
		return
	}
	d.AddHour(num)
	d.Minute = 0
	d.Second = 0
}

// AdvanceMinuteBy advances the minute of hour by the given number. Negative numbers are not allowed and will leave this
// date unchanged.
// AdvanceMinuteBy 将一小时的分钟推进给定数值（并把秒清零）。不允许负数，
// 负数时本日期保持不变。
func (d *Date) AdvanceMinuteBy(num int) {
	if num <= 0 {
		return
	}
	d.AddMinute(num)
	d.Second = 0
}
