package pairing

import (
	"sync"
	"time"
)

// nonceCache remembers recently-seen probe nonces so a captured probe replayed
// inside the freshness window is still rejected. Entries older than the window are
// evicted lazily on each check, so the map stays bounded to one window of traffic.
type nonceCache struct {
	mu     sync.Mutex
	seen   map[string]time.Time
	window time.Duration
}

func newNonceCache(window time.Duration) *nonceCache {
	if window <= 0 {
		window = DefaultReplayWindow
	}
	return &nonceCache{seen: make(map[string]time.Time), window: window}
}

// seenBefore records the nonce and reports whether it had already been seen within
// the window (i.e. a replay). It also evicts expired entries on the way through.
func (c *nonceCache) seenBefore(nonce string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for n, t := range c.seen {
		if now.Sub(t) > c.window {
			delete(c.seen, n)
		}
	}
	if _, ok := c.seen[nonce]; ok {
		return true
	}
	c.seen[nonce] = now
	return false
}
