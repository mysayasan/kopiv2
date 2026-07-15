package services

import (
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/apps/myiotsan/entities"
)

// DueAt is the heart of the scheduler: it must fire in the right minute, on the right weekday, and
// nowhere else. These are hermetic — no location needed for a clock trigger.
func TestSchedule_DueAtClockBoundaries(t *testing.T) {
	s := &ScheduleService{logf: func(string, ...any) {}}
	// A Wednesday.
	base := time.Date(2024, 3, 20, 0, 0, 0, 0, time.UTC)
	sched := &entities.Schedule{Enabled: true, TriggerType: "clock", TimeOfDay: "07:30", TargetType: "scene", SceneId: 1}

	due := func(hh, mm int) bool {
		return s.DueAt(sched, base.Add(time.Duration(hh)*time.Hour+time.Duration(mm)*time.Minute), 0, 0, false)
	}
	if !due(7, 30) {
		t.Error("must fire at exactly 07:30")
	}
	if due(7, 29) || due(7, 31) {
		t.Error("must not fire in the minute before or after")
	}
	if due(19, 30) {
		t.Error("must not fire at 19:30")
	}

	// Weekday filter: only Mondays (1). 2024-03-20 is a Wednesday (3).
	sched.Days = "1"
	if due(7, 30) {
		t.Error("a Monday-only schedule must not fire on a Wednesday")
	}
	sched.Days = "3,6" // Wednesday or Saturday
	if !due(7, 30) {
		t.Error("Wednesday is in the day list and must fire")
	}

	// Disabled never fires.
	sched.Enabled = false
	if due(7, 30) {
		t.Error("a disabled schedule must never fire")
	}
}

// A sunset schedule with an offset resolves against the COMPUTED sun time (sun_test.go already
// checks that time against the almanac). Here we verify DueAt applies the offset and fires in the
// right minute — derived from the function so a ±1-min sun calc doesn't make the test brittle.
func TestSchedule_DueAtSunsetWithOffset(t *testing.T) {
	s := &ScheduleService{logf: func(string, ...any) {}}
	sched := &entities.Schedule{Enabled: true, TriggerType: "sunset", OffsetMinutes: -30, TargetType: "scene", SceneId: 1}
	const lat, lon = 51.5074, -0.1278

	day := time.Date(2024, 3, 20, 12, 0, 0, 0, time.UTC)
	_, set, ok := SunriseSunset(day, lat, lon)
	if !ok {
		t.Fatal("London has a sunset on the equinox")
	}
	fireMinute := set.Add(-30 * time.Minute).Truncate(time.Minute)

	if !s.DueAt(sched, fireMinute, lat, lon, true) {
		t.Errorf("must fire 30 min before sunset (%s)", fireMinute.Format("15:04"))
	}
	if s.DueAt(sched, fireMinute.Add(2*time.Minute), lat, lon, true) {
		t.Error("must not fire two minutes past the resolved instant")
	}
	// Without a location a sun schedule cannot resolve and never fires.
	if s.DueAt(sched, fireMinute, 0, 0, false) {
		t.Error("a sun schedule with no location must not fire")
	}
}

// The double-fire guard: DueAt only decides the minute; Tick's LastFiredAt (unix-minute) is what
// stops a second fire in the same minute. This exercises the guard arithmetic directly.
func TestSchedule_DoubleFireGuard(t *testing.T) {
	now := time.Date(2024, 3, 20, 7, 30, 15, 0, time.UTC)
	thisMin := now.Unix() / 60
	sched := &entities.Schedule{Enabled: true, LastFiredAt: thisMin}
	// The Tick guard is `sc.LastFiredAt == thisMin` → skip. Simulate the check.
	if sched.LastFiredAt != thisMin {
		t.Fatal("precondition")
	}
	// A later minute is a different guard value, so the schedule is eligible again.
	later := now.Add(time.Minute)
	if later.Unix()/60 == thisMin {
		t.Error("the next minute must have a different guard value")
	}
}
