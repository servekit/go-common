package cronx

import (
	"testing"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOnlyWorkdays(t *testing.T) {
	s := OnlyWorkdays(everyDay())

	// Friday → base returns Saturday, skip to Monday
	fri := time.Date(2026, 6, 5, 9, 0, 0, 0, time.Local) // Friday
	next := s.Next(fri)
	assert.Equal(t, time.Monday, next.Weekday())

	// Monday → base returns Tuesday
	mon := time.Date(2026, 6, 8, 9, 0, 0, 0, time.Local) // Monday
	next = s.Next(mon)
	assert.Equal(t, time.Tuesday, next.Weekday())
}

func TestOnlyWeekends(t *testing.T) {
	s := OnlyWeekends(everyDay())

	// Friday → base returns Saturday
	fri := time.Date(2026, 6, 5, 9, 0, 0, 0, time.Local) // Friday
	next := s.Next(fri)
	assert.Equal(t, time.Saturday, next.Weekday())

	// Saturday → base returns Sunday
	sat := time.Date(2026, 6, 6, 9, 0, 0, 0, time.Local)
	next = s.Next(sat)
	assert.Equal(t, time.Sunday, next.Weekday())

	// Sunday → base returns Monday, skip to Saturday
	sun := time.Date(2026, 6, 7, 9, 0, 0, 0, time.Local)
	next = s.Next(sun)
	assert.Equal(t, time.Saturday, next.Weekday())
}

func TestOnlyWorkdays_Integration(t *testing.T) {
	c, err := New(&Config{})
	require.NoError(t, err)

	fired := make(chan time.Weekday, 10)
	c.Schedule(OnlyWorkdays(&testSecondSchedule{}), cron.FuncJob(func() {
		select {
		case fired <- time.Now().Weekday():
		default:
		}
	}))

	c.Start()
	defer c.Stop()

	select {
	case w := <-fired:
		assert.True(t, w >= time.Monday && w <= time.Friday, "expected workday, got %s", w)
	case <-time.After(2 * time.Second):
		t.Fatal("expected job to fire within 2 seconds")
	}
}

// testSecondSchedule fires every second for integration tests.
type testSecondSchedule struct{}

func (s *testSecondSchedule) Next(t time.Time) time.Time {
	return t.Add(time.Second)
}

func everyDay() cron.Schedule {
	return cron.Every(24 * time.Hour)
}
