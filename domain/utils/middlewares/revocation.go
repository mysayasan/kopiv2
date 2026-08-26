package middlewares

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/infra/cache"
)

// Noticing, at a RELYING app, that the identity server ended a session.
//
// THE DEFECT THIS EXISTS FOR. myidsan and a relying app are separate processes with separate
// caches. At the end of the authorization-code flow the relying app is handed myidsan's
// session id, and then mints its OWN token under its OWN signing key and caches its own
// session entry under its own TTL — three days by default. So `validateSession` above, which
// answers entirely from this app's cache, cannot possibly notice a revocation that happened
// somewhere else.
//
// A live two-process bench measured exactly that: an administrator revoked every session for
// an account, the listing said so, the session went 401 at myidsan — and the same browser
// cookie kept working at the fleet console, with full access, indefinitely. Session
// administration was ending sessions at the identity server and nowhere else.
//
// THE SHAPE OF THE FIX, and the two judgement calls in it:
//
//   - ASK ON A TTL, NOT PER REQUEST. Every request re-checking with the identity server would
//     put an HTTP round trip in front of every API call and make the relying app unusable
//     whenever the IdP is slow. The interval is `sso.policyCacheTtlSeconds` (30s by default) —
//     a knob that already existed in the config and, until now, drove nothing at all.
//   - FAIL OPEN WHEN THE IDENTITY SERVER CANNOT BE REACHED, fail closed on a definite "not
//     active". This is a real tradeoff and it is made deliberately. Failing closed would mean
//     every myidsan restart, network blip or certificate hiccup signs the entire estate out of
//     the fleet console — the tool people need most during exactly those incidents. Failing
//     open means an attacker who both holds a stolen cookie AND can partition the relying app
//     from the identity server keeps access until the token expires. The first failure happens
//     regularly and hurts every user; the second requires an attacker who already has
//     substantial network control. Unreachability is logged so it is at least visible.
//
// Once a definite revocation comes back, the local session entry is DELETED rather than just
// refused for this request: from then on the ordinary cache check refuses it with no further
// round trips, and the answer cannot flip back if the identity server becomes unreachable a
// moment later.

// RevocationChecker re-asks an identity server whether a session is still live.
type RevocationChecker struct {
	// Ask returns (active, reachable). `reachable` false means "no answer" — the transport
	// failed, the server errored — and is NEVER treated as a revocation.
	Ask func(ctx context.Context, sessionId string) (active bool, reachable bool)
	// Interval is how long a positive answer is trusted before asking again.
	Interval time.Duration

	mu     sync.Mutex
	recent map[string]time.Time
	// warned throttles the unreachable-identity-server log so a sustained outage produces a
	// line every Interval rather than one per request.
	warned time.Time
}

// NewRevocationChecker builds a checker. A nil ask, or a non-positive interval, yields a
// checker that never refuses anything — an app that has not wired this behaves exactly as it
// did before.
func NewRevocationChecker(ask func(ctx context.Context, sessionId string) (bool, bool), interval time.Duration) *RevocationChecker {
	if ask == nil || interval <= 0 {
		return nil
	}
	return &RevocationChecker{Ask: ask, Interval: interval, recent: map[string]time.Time{}}
}

// stillActive reports whether the session may continue to be served.
func (c *RevocationChecker) stillActive(ctx context.Context, store cache.Store, entry *SessionCacheEntry) bool {
	if c == nil || c.Ask == nil || entry == nil || entry.SessionId == "" {
		return true
	}
	if !c.due(entry.SessionId) {
		return true
	}

	active, reachable := c.Ask(ctx, entry.SessionId)
	if !reachable {
		// No answer is not a revocation. Keep serving, say so, and try again next interval —
		// the session id is deliberately NOT marked as checked, so recovery is immediate.
		c.warnUnreachable()
		return true
	}
	if active {
		c.markChecked(entry.SessionId)
		return true
	}

	// A definite revocation. Drop the local entry so every later request is refused by the
	// ordinary cache check, with no round trip and no way for the verdict to flip back.
	if store != nil {
		if err := store.Delete(ctx, sessionCacheKey(entry.SessionId)); err != nil {
			log.Printf("revoked session %s could not be cleared from the local cache: %v",
				entry.SessionId, err)
		}
	}
	c.forget(entry.SessionId)
	return false
}

// due reports whether this session is out of its trust interval. It also prunes entries that
// are themselves older than the interval, so the map tracks live sessions rather than growing
// once per session id ever seen.
func (c *RevocationChecker) due(sessionId string) bool {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.recent) > 4096 {
		for id, at := range c.recent {
			if now.Sub(at) > c.Interval {
				delete(c.recent, id)
			}
		}
	}
	last, ok := c.recent[sessionId]
	return !ok || now.Sub(last) >= c.Interval
}

func (c *RevocationChecker) markChecked(sessionId string) {
	c.mu.Lock()
	c.recent[sessionId] = time.Now()
	c.mu.Unlock()
}

func (c *RevocationChecker) forget(sessionId string) {
	c.mu.Lock()
	delete(c.recent, sessionId)
	c.mu.Unlock()
}

func (c *RevocationChecker) warnUnreachable() {
	now := time.Now()
	c.mu.Lock()
	quiet := now.Sub(c.warned) < c.Interval
	if !quiet {
		c.warned = now
	}
	c.mu.Unlock()
	if !quiet {
		log.Printf("identity server unreachable for session revocation checks — " +
			"sessions continue to be served until it answers again")
	}
}
