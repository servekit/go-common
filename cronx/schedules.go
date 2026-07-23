package cronx

import (
	"time"

	"github.com/robfig/cron/v3"
)

// dayFilter wraps a Schedule to skip days that don't match a predicate.
type dayFilter struct {
	inner cron.Schedule
	match func(time.Weekday) bool
}

// OnlyWorkdays wraps a Schedule to skip weekends (Saturday and Sunday).
func OnlyWorkdays(s cron.Schedule) cron.Schedule {
	return &dayFilter{inner: s, match: isWorkday}
}

// OnlyWeekends wraps a Schedule to skip workdays (Monday through Friday).
func OnlyWeekends(s cron.Schedule) cron.Schedule {
	return &dayFilter{inner: s, match: isWeekend}
}

func (d *dayFilter) Next(t time.Time) time.Time {
	for {
		next := d.inner.Next(t)
		if d.match(next.Weekday()) {
			return next
		}
		t = next
	}
}

func isWorkday(w time.Weekday) bool {
	return w >= time.Monday && w <= time.Friday
}

func isWeekend(w time.Weekday) bool {
	return w == time.Saturday || w == time.Sunday
}
