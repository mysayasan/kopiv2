package services

import (
	"context"
	"testing"
	"time"

	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
)

// The cache clock is the number `Decide()`'s TTL comparison was missing. These tests pin the two
// properties that make it worth having, and one that makes it safe.

func newClock(t *testing.T) (*CacheClock, *fakeSettingsRepo) {
	t.Helper()
	repo := newFakeSettingsRepo()
	return NewCacheClock(repo), repo
}

// TestCacheClockMeasuresTimeSinceContact is the whole point: an age that grows.
func TestCacheClockMeasuresTimeSinceContact(t *testing.T) {
	c, _ := newClock(t)
	now := time.Unix(1_700_000_000, 0)
	c.now = func() time.Time { return now }

	c.Touch(context.Background())
	if age := c.Age(context.Background()); age != 0 {
		t.Fatalf("age immediately after contact = %s, want 0", age)
	}

	now = now.Add(90 * time.Minute)
	if age := c.Age(context.Background()); age != 90*time.Minute {
		t.Fatalf("age = %s, want 90m", age)
	}

	// Contact resets it. This is what stops a controller that is being actively administered — or
	// whose control plane is reachable — from expiring for a staleness that does not exist.
	c.Touch(context.Background())
	if age := c.Age(context.Background()); age != 0 {
		t.Fatalf("age after fresh contact = %s, want 0", age)
	}
}

// TestCacheClockSurvivesARestart is the scenario the persistence exists for.
//
// A door controller rebooting while cut off from its control plane is not an edge case, it is the
// case: power blip, watchdog, an installer power-cycling the panel. An in-memory clock would hand a
// replica that has been unreachable for a week a fresh 72 hours on every restart, which turns the
// TTL into a formality anybody can reset by pulling the plug.
func TestCacheClockSurvivesARestart(t *testing.T) {
	repo := newFakeSettingsRepo()
	first := NewCacheClock(repo)
	start := time.Unix(1_700_000_000, 0)
	first.now = func() time.Time { return start }
	first.Touch(context.Background())

	// A new process, a week later, over the same database.
	second := NewCacheClock(repo)
	later := start.Add(7 * 24 * time.Hour)
	second.now = func() time.Time { return later }

	if age := second.Age(context.Background()); age != 7*24*time.Hour {
		t.Fatalf("age after restart = %s, want 168h — the clock restarted at zero", age)
	}
}

// TestCacheClockIgnoresAClockThatWentBackwards. An appliance with no RTC gets its time from NTP
// after boot, and the step can be large and negative. A negative age is a measurement fault, and
// the wrong way to be wrong here is to read it as a number and deny a whole site on it.
func TestCacheClockIgnoresAClockThatWentBackwards(t *testing.T) {
	c, _ := newClock(t)
	now := time.Unix(1_700_000_000, 0)
	c.now = func() time.Time { return now }
	c.Touch(context.Background())

	now = now.Add(-48 * time.Hour)
	if age := c.Age(context.Background()); age != 0 {
		t.Fatalf("age with a backwards clock = %s, want 0", age)
	}
	// And it re-anchors, so the next measurement is against the corrected time rather than being
	// permanently negative.
	now = now.Add(2 * time.Hour)
	if age := c.Age(context.Background()); age != 2*time.Hour {
		t.Fatalf("age after re-anchoring = %s, want 2h", age)
	}
}

// TestCacheClockStartsFreshOnAnApplianceThatHasNeverBeenTouched. The row is absent only before
// the first contact has ever been recorded, which is during commissioning. Reporting an infinite
// age there would deny every door on an appliance that is by definition not running on anybody's
// stale data.
func TestCacheClockStartsFreshOnAnApplianceThatHasNeverBeenTouched(t *testing.T) {
	c, repo := newClock(t)
	now := time.Unix(1_700_000_000, 0)
	c.now = func() time.Time { return now }

	if age := c.Age(context.Background()); age != 0 {
		t.Fatalf("age on a virgin appliance = %s, want 0", age)
	}
	// It also starts the clock, so an appliance nobody ever administers does eventually go stale
	// rather than staying fresh forever.
	if len(repo.rows) == 0 {
		t.Fatal("the first read did not start the clock; the appliance would never go stale")
	}
	now = now.Add(time.Hour)
	if age := c.Age(context.Background()); age != time.Hour {
		t.Fatalf("age = %s, want 1h", age)
	}
}

// TestNilCacheClockIsSafe: a controller assembled without one (every unit test, and any future
// embedding) has no staleness to report and must not panic.
func TestNilCacheClockIsSafe(t *testing.T) {
	var c *CacheClock
	c.Touch(context.Background())
	if age := c.Age(context.Background()); age != 0 {
		t.Fatalf("nil clock age = %s, want 0", age)
	}
	if fn := c.AgeFunc(); fn() != 0 {
		t.Fatal("nil clock AgeFunc did not report zero")
	}
}

// TestCacheClockThrottlesWrites keeps the clock off the write path of every rule edit and every
// control-channel frame. The in-memory value is always current; only the persisted one lags.
func TestCacheClockThrottlesWrites(t *testing.T) {
	c, repo := newClock(t)
	now := time.Unix(1_700_000_000, 0)
	c.now = func() time.Time { return now }

	for i := 0; i < 50; i++ {
		now = now.Add(time.Second)
		c.Touch(context.Background())
	}
	if repo.writes > 2 {
		t.Fatalf("%d writes for 50 touches inside a minute; the throttle is not working", repo.writes)
	}
	if age := c.Age(context.Background()); age != 0 {
		t.Fatalf("age = %s, want 0 — the throttle must not delay the in-memory value", age)
	}
}

// --- a minimal RuntimeSetting repo -------------------------------------------------------------

type fakeSettingsRepo struct {
	rows   map[string]sharedentities.RuntimeSetting
	nextId int64
	writes int
}

func newFakeSettingsRepo() *fakeSettingsRepo {
	return &fakeSettingsRepo{rows: map[string]sharedentities.RuntimeSetting{}}
}

func (f *fakeSettingsRepo) GetByUnique(ctx context.Context, datasrc string, keyGroup string, uids ...any) (*sharedentities.RuntimeSetting, error) {
	if len(uids) == 0 {
		return nil, errNoResult{}
	}
	row, ok := f.rows[toStr(uids[0])]
	if !ok {
		return nil, errNoResult{}
	}
	out := row
	return &out, nil
}

func (f *fakeSettingsRepo) Create(ctx context.Context, group string, row sharedentities.RuntimeSetting) (uint64, error) {
	f.nextId++
	row.Id = f.nextId
	f.rows[row.Key] = row
	f.writes++
	return uint64(row.Id), nil
}

func (f *fakeSettingsRepo) UpdateById(ctx context.Context, group string, row sharedentities.RuntimeSetting) (uint64, error) {
	f.rows[row.Key] = row
	f.writes++
	return 1, nil
}

func toStr(v any) string {
	s, _ := v.(string)
	return s
}

// errNoResult matches what isNotFound recognises, so the clock takes the "nothing stored yet" path
// rather than the "the database is broken" one.
type errNoResult struct{}

func (errNoResult) Error() string { return "no result found" }
