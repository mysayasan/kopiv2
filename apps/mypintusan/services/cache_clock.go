package services

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
)

// CacheClock answers the one question `Decide()`'s GATE 10 asks and nothing on this appliance could
// previously answer: how long has this controller been deciding without contact with an authority
// over its access data?
//
// WHY IT DID NOT EXIST. `ControllerConfig.CacheAge` has always been declared, and `Decide()` has
// always compared it against the door's TTL. It was never assigned anywhere outside a unit test.
// In production `c.cfg.CacheAge` was nil, so `Snapshot.CacheAge` was always zero, so
// `s.CacheAge > ttl` was always false — and `offline-cache-expired`, the reason code the whole
// offline design turns on, could not be produced on any install. The first live bench of offline
// mode measured it: a door 20 seconds past a 2-second TTL still granted.
//
// This is the same shape as the three unreachable alarms in #220. The gate was right, the reason
// code was right, the four translations were right, and the number the comparison needed was never
// computed by anything.
//
// WHAT COUNTS AS CONTACT. The clock resets when somebody who is entitled to say who may enter has
// reached this controller:
//
//   - the fleet control channel connecting to its control plane — a node whose parent can reach it
//     is a node a revocation can reach;
//   - an administrative change to the access data on this appliance itself — a holder, credential,
//     group, schedule, grant or door. An operator standing at the controller changing the rules is
//     the most direct source of truth there is.
//
// A CONTROLLER THAT NOBODY CAN REACH IS THE CASE THE TTL EXISTS FOR, and the second signal is what
// keeps that from being absurd: an appliance being actively administered never expires, while one
// sitting cut off with nobody touching it eventually stops honouring rules it can no longer be told
// have changed. That is what "past the TTL the door denies" means, and it is the only defence
// against the attack the design names — cut the uplink and wait.
//
// IT IS PERSISTED, and that is not an optimisation. A door controller rebooting while cut off is
// precisely the scenario; an in-memory clock would restart at zero every time and hand a stale
// replica a fresh 72 hours on every power cycle. Writes are throttled — the clock is touched on
// every rule edit and every reconnect, and none of that is worth an SQL write per event.
type CacheClock struct {
	repo settingsRowRepo
	now  func() time.Time

	mu sync.Mutex
	// last is the most recent contact. Zero means "not yet loaded from the database".
	last time.Time
	// written is what `last` was when it was last persisted, so the throttle knows what the
	// database already holds rather than how long ago it wrote.
	written time.Time
}

// settingsRowRepo is the three methods this clock needs from the runtime-settings table. Narrow on
// purpose: the generic repo has sixteen, and a test that has to implement all of them to pin a
// timestamp is a test nobody writes.
type settingsRowRepo interface {
	GetByUnique(ctx context.Context, datasrc string, keyGroup string, uids ...any) (*sharedentities.RuntimeSetting, error)
	Create(ctx context.Context, datasrc string, model sharedentities.RuntimeSetting) (uint64, error)
	UpdateById(ctx context.Context, datasrc string, model sharedentities.RuntimeSetting) (uint64, error)
}

// cacheClockKey is the runtime_setting row holding the last-contact timestamp.
const cacheClockKey = "access.lastContact"

// cacheClockPersistEvery bounds how far the persisted value may lag the in-memory one. A crash can
// therefore lose up to this much — which errs towards a LARGER measured age after a restart, i.e.
// towards denying. The safe direction is the only acceptable one for a rounding error here.
const cacheClockPersistEvery = time.Minute

// NewCacheClock builds the clock over the runtime-settings table.
func NewCacheClock(repo settingsRowRepo) *CacheClock {
	return &CacheClock{repo: repo, now: time.Now}
}

// Touch records contact with an authority over the access data.
//
// Nil-safe: a controller assembled without a clock (every unit test, and any future embedding)
// simply has no staleness to report, which is the behaviour those callers had before it existed.
func (c *CacheClock) Touch(ctx context.Context) {
	if c == nil {
		return
	}
	now := c.now()

	c.mu.Lock()
	c.last = now
	due := now.Sub(c.written) >= cacheClockPersistEvery
	if due {
		c.written = now
	}
	c.mu.Unlock()

	if due {
		c.persist(ctx, now)
	}
}

// Age reports how long it has been since the last contact.
//
// A clock with no record at all returns 0 rather than infinity. That is deliberate and it is the
// fail-SAFE direction for this particular unknown: the row is absent only before the first contact
// has ever been recorded, which on a fresh appliance is the moment it is being installed. Denying
// every door on a controller that has never been administered would brick the commissioning visit,
// and the appliance is by definition not yet running on anybody's stale data.
func (c *CacheClock) Age(ctx context.Context) time.Duration {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	last := c.last
	c.mu.Unlock()

	if last.IsZero() {
		last = c.load(ctx)
		if last.IsZero() {
			// Nothing recorded yet. Start the clock now so the very next decision is measured
			// against something real, and so a controller that is never administered again does
			// eventually go stale rather than staying fresh forever.
			c.Touch(ctx)
			return 0
		}
		c.mu.Lock()
		if c.last.IsZero() {
			c.last, c.written = last, last
		}
		last = c.last
		c.mu.Unlock()
	}

	age := c.now().Sub(last)
	if age < 0 {
		// A clock that went backwards (NTP stepping the appliance's time, which happens on an
		// embedded box with no RTC) must not read as a fresh cache OR as an expired one. Treat it
		// as fresh and re-anchor: a negative age is a measurement fault, not evidence of staleness,
		// and denying a whole site on it would be the wrong way to be wrong.
		c.Touch(ctx)
		return 0
	}
	return age
}

// AgeFunc returns the accessor the controller wants, bound to a background context.
//
// The decision path has a request context, but it is the badge's context: cancelling it must not
// abandon a write of the last-contact timestamp, and it is torn down long before the next decision.
func (c *CacheClock) AgeFunc() func() time.Duration {
	return func() time.Duration { return c.Age(context.Background()) }
}

func (c *CacheClock) load(ctx context.Context) time.Time {
	if c.repo == nil {
		return time.Time{}
	}
	// GetByUnique against the "key" ukey, which RuntimeSetting genuinely declares. A key group that
	// matches no ukey falls through to an unfiltered select and returns the FIRST ROW IN THE TABLE —
	// the bug that once made everyone in this suite a superadmin.
	row, err := c.repo.GetByUnique(ctx, "", "key", cacheClockKey)
	if err != nil || row == nil {
		return time.Time{}
	}
	secs, err := strconv.ParseInt(row.Value, 10, 64)
	if err != nil || secs <= 0 {
		return time.Time{}
	}
	return time.Unix(secs, 0)
}

func (c *CacheClock) persist(ctx context.Context, at time.Time) {
	if c.repo == nil {
		return
	}
	value := strconv.FormatInt(at.UTC().Unix(), 10)
	now := at.UTC().Unix()

	existing, err := c.repo.GetByUnique(ctx, "", "key", cacheClockKey)
	if err != nil && !isNotFound(err) {
		return
	}
	if existing != nil {
		existing.Value = value
		existing.UpdatedAt = now
		_, _ = c.repo.UpdateById(ctx, "", *existing)
		return
	}
	_, _ = c.repo.Create(ctx, "", sharedentities.RuntimeSetting{
		Key: cacheClockKey, Value: value,
		CreatedAt: now, UpdatedAt: now,
	})
}

// String makes the clock readable in a log line without exposing the mutex.
func (c *CacheClock) String() string {
	if c == nil {
		return "cache clock: none"
	}
	return fmt.Sprintf("cache clock: last contact %s ago", c.Age(context.Background()).Truncate(time.Second))
}
