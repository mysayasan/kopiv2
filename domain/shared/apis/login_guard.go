package apis

import (
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/infra/cache"
	"github.com/mysayasan/kopiv2/infra/config"
)

// LoginGuardConfig tunes the failed-login lockout. Zero values are filled with
// safe defaults by NewLoginGuard, so a partially-specified config still works.
type LoginGuardConfig struct {
	// Enabled turns the guard on. A disabled guard is a no-op (never locks).
	Enabled bool
	// MaxAttempts is the number of failures within Window that trips a lockout.
	MaxAttempts int
	// Window is the rolling span over which failures are counted.
	Window time.Duration
	// BaseLockout is the first lockout duration. Each repeated lockout for the
	// same key doubles it (escalating backoff) up to MaxLockout.
	BaseLockout time.Duration
	// MaxLockout caps the escalating backoff.
	MaxLockout time.Duration
	// FailedDelay is added to every failed attempt to slow online guessing.
	FailedDelay time.Duration
}

func (c LoginGuardConfig) withDefaults() LoginGuardConfig {
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = 5
	}
	if c.Window <= 0 {
		c.Window = 5 * time.Minute
	}
	if c.BaseLockout <= 0 {
		c.BaseLockout = time.Minute
	}
	if c.MaxLockout <= 0 {
		c.MaxLockout = 15 * time.Minute
	}
	if c.MaxLockout < c.BaseLockout {
		c.MaxLockout = c.BaseLockout
	}
	return c
}

// guardEntry holds the failure/lockout state for a single key (an IP or an
// account). lockoutCount drives the escalating backoff and only resets when the
// entry is pruned after a long idle period.
type guardEntry struct {
	failures     int
	windowStart  time.Time
	lockedUntil  time.Time
	lockoutCount int
	lastSeen     time.Time
}

// LoginGuard is a thread-safe, in-memory failed-login tracker. State is keyed by
// arbitrary strings (the middleware uses one IP key and one account key) so a
// lockout on either axis blocks the attempt. It deliberately keeps no I/O: the
// caller decides how to respond and whether to notify.
type LoginGuard struct {
	mu      sync.Mutex
	cfg     LoginGuardConfig
	entries map[string]*guardEntry
	now     func() time.Time

	// store, when set, mirrors the same state into a shared cache so a lockout spans every
	// instance of a clustered deployment. See login_guard_shared.go — the in-memory map
	// above keeps working exactly as before either way, and is what the deployment falls
	// back to if the cache is unreachable.
	store     cache.Store
	namespace string
}

// NewLoginGuardFor builds a guard straight from the app's resolved loginSecurity block.
//
// Here rather than in each app's wiring because it is the same mapping every time and the
// two apps that need it are the two whose lockouts have to agree. myseliasan grew a lockout
// second (it had none at all) and the first thing it needed was this function, already
// written once inside myidsan's app package.
//
// Note that Effective() resolves an ABSENT loginSecurity block to ON with defaults, so an app
// that never mentions the block still gets a lockout — which is why myseliasan's config, which
// has no such block, nonetheless already promised one.
func NewLoginGuardFor(ls config.EffectiveLoginSecurity) *LoginGuard {
	return NewLoginGuard(LoginGuardConfig{
		Enabled:     ls.Enabled,
		MaxAttempts: ls.MaxAttempts,
		Window:      time.Duration(ls.WindowSeconds) * time.Second,
		BaseLockout: time.Duration(ls.LockoutSeconds) * time.Second,
		MaxLockout:  time.Duration(ls.LockoutMaxSeconds) * time.Second,
		FailedDelay: time.Duration(ls.FailedDelayMs) * time.Millisecond,
	})
}

// NewLoginGuard builds a guard from cfg. A nil result is never returned; callers
// can treat a guard whose Enabled is false as inert.
func NewLoginGuard(cfg LoginGuardConfig) *LoginGuard {
	return &LoginGuard{
		cfg:     cfg.withDefaults(),
		entries: make(map[string]*guardEntry),
		now:     time.Now,
	}
}

// FailedDelay exposes the configured per-failure delay so the middleware can
// apply it outside the lock.
func (g *LoginGuard) FailedDelay() time.Duration {
	if g == nil || !g.cfg.Enabled {
		return 0
	}
	return g.cfg.FailedDelay
}

// Locked reports whether any of keys is currently locked and, if so, the longest
// remaining wait across them. A disabled or nil guard never locks.
func (g *LoginGuard) Locked(keys ...string) (bool, time.Duration) {
	if g == nil || !g.cfg.Enabled {
		return false, 0
	}
	// Asked BEFORE taking the mutex: the shared lookup does network I/O, and holding the
	// lock across it would serialise every sign-in on the slowest cache round trip.
	sharedLocked, sharedRetry := g.sharedLocked(keys...)

	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	var maxRetry time.Duration
	locked := false
	for _, key := range keys {
		e := g.entries[key]
		if e == nil {
			continue
		}
		if remaining := e.lockedUntil.Sub(now); remaining > 0 {
			locked = true
			if remaining > maxRetry {
				maxRetry = remaining
			}
		}
	}
	// The OR of the two verdicts. The shared half can only ever add a lockout, never
	// remove one, so a cache that is down or lying leaves the local guard in charge.
	if sharedLocked {
		locked = true
		if sharedRetry > maxRetry {
			maxRetry = sharedRetry
		}
	}
	return locked, maxRetry
}

// RecordFailure registers one failed attempt against every key. It returns
// whether this call newly tripped a lockout (the transition, so the caller can
// notify exactly once) and the longest remaining wait across the keys.
func (g *LoginGuard) RecordFailure(keys ...string) (lockedNow bool, retry time.Duration) {
	if g == nil || !g.cfg.Enabled {
		return false, 0
	}
	sharedNow, sharedRetry := g.sharedRecordFailure(keys...)

	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	g.pruneLocked(now)
	for _, key := range keys {
		e := g.entries[key]
		if e == nil {
			e = &guardEntry{}
			g.entries[key] = e
		}
		e.lastSeen = now
		// Still locked: leave the lockout intact, just surface the wait.
		if remaining := e.lockedUntil.Sub(now); remaining > 0 {
			if remaining > retry {
				retry = remaining
			}
			continue
		}
		if e.windowStart.IsZero() || now.Sub(e.windowStart) > g.cfg.Window {
			e.windowStart = now
			e.failures = 0
		}
		e.failures++
		if e.failures < g.cfg.MaxAttempts {
			continue
		}
		// Threshold crossed: trip an escalating lockout and start fresh.
		e.lockoutCount++
		dur := g.escalatedLockout(e.lockoutCount)
		e.lockedUntil = now.Add(dur)
		e.failures = 0
		e.windowStart = time.Time{}
		lockedNow = true
		if dur > retry {
			retry = dur
		}
	}
	// A lockout the SHARED half tripped is still a transition worth reporting: on a
	// clustered deployment it is the one that actually took effect. Only the call that
	// crosses the threshold reports it — once the shared lock exists every later call takes
	// the already-locked branch — so the caller's "notify on the transition" holds. Two
	// instances crossing in the same instant can both report it, which costs a duplicate
	// audit entry and log line and nothing else.
	if sharedNow {
		lockedNow = true
	}
	if sharedRetry > retry {
		retry = sharedRetry
	}
	return lockedNow, retry
}

// RecordSuccess clears the failure/lockout state for the given keys after a
// successful authentication.
func (g *LoginGuard) RecordSuccess(keys ...string) {
	if g == nil || !g.cfg.Enabled {
		return
	}
	g.sharedRecordSuccess(keys...)

	g.mu.Lock()
	defer g.mu.Unlock()
	for _, key := range keys {
		delete(g.entries, key)
	}
}

// escalatedLockout returns BaseLockout doubled (count-1) times, capped at
// MaxLockout and guarded against shift overflow.
func (g *LoginGuard) escalatedLockout(count int) time.Duration {
	if count <= 1 {
		return g.cfg.BaseLockout
	}
	shift := count - 1
	if shift > 20 {
		return g.cfg.MaxLockout
	}
	dur := g.cfg.BaseLockout << uint(shift)
	if dur <= 0 || dur > g.cfg.MaxLockout {
		return g.cfg.MaxLockout
	}
	return dur
}

// pruneLocked drops entries that are unlocked and idle beyond the escalation
// horizon, keeping the map bounded while letting lockoutCount decay only after a
// genuinely quiet period. The horizon spans the full backoff cap plus one window
// so an attacker who waits out each lockout still keeps escalating. Caller must
// hold g.mu.
func (g *LoginGuard) pruneLocked(now time.Time) {
	horizon := g.cfg.MaxLockout + g.cfg.Window
	for key, e := range g.entries {
		if now.Before(e.lockedUntil) {
			continue
		}
		if now.Sub(e.lastSeen) > horizon {
			delete(g.entries, key)
		}
	}
}
