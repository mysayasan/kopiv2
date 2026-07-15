package services

import (
	"math"
	"testing"
	"time"
)

// Against published almanac times for London (51.5074°N, 0.1278°W). The algorithm is good to ~1–2
// minutes; the tolerance is 5 to leave room for the ±0.833° horizon convention and rounding.
func TestSunriseSunset_London(t *testing.T) {
	const lat, lon = 51.5074, -0.1278
	cases := []struct {
		date             string
		riseUTC, setUTC  string
	}{
		{"2024-03-20", "06:03", "18:14"}, // equinox (GMT, no DST): ~12h day
		{"2024-06-20", "03:43", "20:21"}, // solstice (times are UTC; local was BST)
		{"2024-12-21", "08:04", "15:53"}, // winter solstice: short day
	}
	for _, c := range cases {
		day, _ := time.Parse("2006-01-02", c.date)
		rise, set, ok := SunriseSunset(day, lat, lon)
		if !ok {
			t.Fatalf("%s: sun should rise and set in London", c.date)
		}
		assertClock(t, c.date+" sunrise", rise, c.riseUTC, 5)
		assertClock(t, c.date+" sunset", set, c.setUTC, 5)
	}
}

// Above the Arctic circle at midsummer the sun never sets — the function must say so, not return a
// bogus time a schedule would fire on.
func TestSunriseSunset_PolarDayHasNoEvent(t *testing.T) {
	day, _ := time.Parse("2006-01-02", "2024-06-21")
	if _, _, ok := SunriseSunset(day, 78.0, 15.0); ok { // Svalbard, midnight sun
		t.Fatal("the sun does not set over Svalbard at midsummer; ok must be false")
	}
}

func assertClock(t *testing.T, what string, got time.Time, wantHHMM string, tolMin int) {
	t.Helper()
	want, _ := time.Parse("15:04", wantHHMM)
	gotMin := got.Hour()*60 + got.Minute()
	wantMin := want.Hour()*60 + want.Minute()
	if d := int(math.Abs(float64(gotMin - wantMin))); d > tolMin {
		t.Errorf("%s = %02d:%02d UTC, want ~%s (off by %d min)", what, got.Hour(), got.Minute(), wantHHMM, d)
	}
}
