package apis

import (
	"context"
	"time"

	"github.com/mysayasan/kopiv2/infra/cache"
)

// Sharing the failed-login lockout across instances.
//
// LoginGuard is an in-memory map, which is exactly right for a single process and silently
// wrong for a clustered one. myidsan and myseliasan are documented as "Tier A, genuinely
// clusterable"; myidsan even has a setup-wizard step and a Settings panel for DECLARING the
// deployment clustered. In that deployment every instance keeps its own counters, so:
//
//   - an attacker's budget is MaxAttempts PER INSTANCE per window, not per deployment. A
//     live two-instance bench on one shared Postgres and one shared Redis locked the account
//     on instance A after eight wrong passwords, and instance B then evaluated a ninth
//     normally and allowed a fresh eight of its own;
//   - the lockout does not lock. A legitimate user shut out on one instance simply lands on
//     another and signs in, so the control is not even reliably ON;
//   - none of this was visible. The clustering preflight checklist exists precisely to name
//     per-process state that ought to be shared — it covers the cache, the lock provider,
//     the at-rest key, the JWT secret and the DB pool — and said nothing about this.
//
// The fix keeps the in-memory guard exactly as it was and adds the shared store ALONGSIDE
// it, rather than replacing it. That ordering is deliberate:
//
//   - a locked verdict is the OR of the two, so the shared store can only ever lock more,
//     never less;
//   - if the cache is unreachable, every shared call fails and the guard degrades to
//     precisely today's per-process behaviour. A Redis outage must not be a way to switch
//     the brute-force protection off, and it must not lock everybody out either;
//   - single-instance deployments that pass no store are bit-for-bit unchanged.
//
// The counter itself rides on Store.AllowSlidingWindow, which is a Lua script on Redis and
// therefore atomic across instances. A plain read-modify-write would lose increments under
// exactly the concurrent load an attack produces.

const (
	// guardSharedPrefix namespaces every key this file writes.
	guardSharedPrefix = "loginguard:"
	// guardWindowSuffix is the sliding-window counter of recent failures.
	guardWindowSuffix = ":win"
	// guardLockSuffix holds the unix expiry of an engaged lockout. The value is stored
	// rather than inferred from the TTL because Store exposes no way to read a TTL back,
	// and the caller needs the remaining wait for its Retry-After.
	guardLockSuffix = ":lock"
	// guardEscalationSuffix counts how many times this key has been locked, so the backoff
	// keeps doubling across instances instead of restarting at the base on each one.
	guardEscalationSuffix = ":esc"
)

// sharedGuardTimeout bounds every cache round trip. A sign-in must not hang because the
// cache is slow; on timeout the shared half is skipped and the in-memory half still applies.
const sharedGuardTimeout = 2 * time.Second

// WithSharedStore attaches a shared cache so lockouts span every instance of a clustered
// deployment. Passing nil leaves the guard purely in-memory. Returns the guard so it can be
// chained onto NewLoginGuard.
//
// namespace separates apps that share one cache — a lockout on the identity server is not a
// lockout on the fleet console, and they are different credentials against different stores.
func (g *LoginGuard) WithSharedStore(store cache.Store, namespace string) *LoginGuard {
	if g == nil {
		return g
	}
	g.store = store
	g.namespace = namespace
	return g
}

// SharesState reports whether lockouts are visible to other instances. The deployment
// preflight checklist reads this to tell an operator the truth about their cluster.
func (g *LoginGuard) SharesState() bool {
	return g != nil && g.cfg.Enabled && g.store != nil
}

func (g *LoginGuard) sharedKey(key, suffix string) string {
	return guardSharedPrefix + g.namespace + ":" + key + suffix
}

// sharedWindowLimit converts MaxAttempts into the limit AllowSlidingWindow needs to trip on
// the SAME attempt the in-memory guard does.
//
// The two count differently and the off-by-one is not cosmetic. The in-memory guard
// increments and then trips when the total REACHES MaxAttempts, so with the shipped eight it
// locks on the eighth wrong password. AllowSlidingWindow refuses when the stored count is
// already >= limit, checked BEFORE recording the current attempt, so passing MaxAttempts
// straight through makes it refuse on the NINTH. A live two-instance bench showed exactly
// what that costs: instance A locked at eight, the shared counter sat at 8-of-8 still
// "allowed" so no shared lockout was ever written, and instance B then evaluated one more
// wrong password before its own attempt pushed the shared counter over. One free guess per
// instance is not much — and it is the difference between a control that holds at its stated
// threshold and one that quietly does not.
//
// MaxAttempts of 1 is degenerate here (a limit of 0 disables the window entirely), so it
// floors at 1 and the shared half trips one attempt after the local one. The local half
// still locks that instance immediately, so the deployment is protected either way.
func (g *LoginGuard) sharedWindowLimit() int64 {
	if g.cfg.MaxAttempts <= 1 {
		return 1
	}
	return int64(g.cfg.MaxAttempts - 1)
}

// sharedLocked reports the longest remaining lockout across keys according to the shared
// store. Any error is reported as "not locked" so the in-memory verdict decides alone.
func (g *LoginGuard) sharedLocked(keys ...string) (bool, time.Duration) {
	if g.store == nil {
		return false, 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), sharedGuardTimeout)
	defer cancel()

	now := g.now()
	locked := false
	var maxRetry time.Duration
	for _, key := range keys {
		var until int64
		found, err := g.store.Get(ctx, g.sharedKey(key, guardLockSuffix), &until)
		if err != nil || !found {
			continue
		}
		if remaining := time.Unix(until, 0).Sub(now); remaining > 0 {
			locked = true
			if remaining > maxRetry {
				maxRetry = remaining
			}
		}
	}
	return locked, maxRetry
}

// sharedRecordFailure advances the deployment-wide counter for every key and engages a
// shared lockout on the key that crosses the threshold.
func (g *LoginGuard) sharedRecordFailure(keys ...string) (lockedNow bool, retry time.Duration) {
	if g.store == nil {
		return false, 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), sharedGuardTimeout)
	defer cancel()

	now := g.now()
	for _, key := range keys {
		// Already locked here: leave it alone and surface the wait. Re-tripping would
		// escalate the backoff once per attempt rather than once per lockout.
		var until int64
		if found, err := g.store.Get(ctx, g.sharedKey(key, guardLockSuffix), &until); err == nil && found {
			if remaining := time.Unix(until, 0).Sub(now); remaining > 0 {
				if remaining > retry {
					retry = remaining
				}
				continue
			}
		}

		res, err := g.store.AllowSlidingWindow(ctx, g.sharedKey(key, guardWindowSuffix),
			g.sharedWindowLimit(), g.cfg.Window, now)
		if err != nil {
			// The cache is unreachable or misbehaving. The in-memory guard has already
			// counted this attempt, so the deployment falls back to per-process throttling
			// rather than to none.
			continue
		}
		if res.Allowed {
			continue
		}

		// Threshold crossed. Escalate from the count this deployment has seen, not this
		// process's — an attacker rotating instances would otherwise reset the backoff to
		// the base on every hop.
		var escalation int64
		if found, err := g.store.Get(ctx, g.sharedKey(key, guardEscalationSuffix), &escalation); err != nil || !found {
			escalation = 0
		}
		escalation++
		dur := g.escalatedLockout(int(escalation))

		if err := g.store.Set(ctx, g.sharedKey(key, guardLockSuffix),
			now.Add(dur).Unix(), dur); err != nil {
			continue
		}
		// The escalation counter outlives the lockout it produced, so waiting one out and
		// starting again keeps doubling. It expires after the horizon the in-memory guard
		// prunes on, so a genuinely quiet key eventually forgets.
		_ = g.store.Set(ctx, g.sharedKey(key, guardEscalationSuffix), escalation,
			g.cfg.MaxLockout+g.cfg.Window)
		// The window is spent: clear it so the next lockout is counted from a fresh one.
		_ = g.store.Delete(ctx, g.sharedKey(key, guardWindowSuffix))

		lockedNow = true
		if dur > retry {
			retry = dur
		}
	}
	return lockedNow, retry
}

// sharedRecordSuccess clears the deployment-wide state for these keys. The escalation
// counter goes too: a correct credential is proof this key is not mid-attack, and keeping
// it would punish the account's owner for somebody else's guessing.
func (g *LoginGuard) sharedRecordSuccess(keys ...string) {
	if g.store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), sharedGuardTimeout)
	defer cancel()
	for _, key := range keys {
		_ = g.store.Delete(ctx, g.sharedKey(key, guardWindowSuffix))
		_ = g.store.Delete(ctx, g.sharedKey(key, guardLockSuffix))
		_ = g.store.Delete(ctx, g.sharedKey(key, guardEscalationSuffix))
	}
}
