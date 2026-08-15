package notification

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// SSEChannel delivers notifications to connected browsers over Server-Sent
// Events. It is both a Channel (the hub sends to it) and an http.Handler (each
// browser connects to it). Broadcasts are non-blocking: a client whose buffer is
// full is skipped for that message rather than stalling the publisher.
type SSEChannel struct {
	mu        sync.RWMutex
	clients   map[int]chan Notification
	nextID    int
	clientBuf int
	closed    bool
}

// SSEOptions configures the SSE channel.
type SSEOptions struct {
	// ClientBuffer is the per-connection queue depth. Defaults to 16.
	ClientBuffer int
}

// NewSSEChannel creates an SSE channel ready to register on the hub and mount as
// an HTTP handler.
func NewSSEChannel(opts SSEOptions) *SSEChannel {
	buf := opts.ClientBuffer
	if buf <= 0 {
		buf = 16
	}
	return &SSEChannel{
		clients:   map[int]chan Notification{},
		clientBuf: buf,
	}
}

func (c *SSEChannel) Name() string { return "sse" }

// Send broadcasts to every connected client without blocking.
func (c *SSEChannel) Send(_ context.Context, n Notification) error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, ch := range c.clients {
		select {
		case ch <- n:
		default:
			// Slow client: drop this message for them rather than blocking.
		}
	}
	return nil
}

// ServeHTTP streams notifications to one client until the request is cancelled.
func (c *SSEChannel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	id, ch, ok := c.subscribe()
	if !ok {
		http.Error(w, "notifications stream closed", http.StatusServiceUnavailable)
		return
	}
	defer c.unsubscribe(id)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// This response needs a ROLLING write deadline, not the server's fixed one and not none
	// at all.
	//
	// The server sets WriteTimeout (30s by default) as an ABSOLUTE deadline from the start
	// of the request, and it does not reset when more is written. A stream is therefore
	// killed 30 seconds after it opens no matter how much traffic it carries — and the
	// symptom is close to invisible, because a browser's EventSource silently reconnects:
	// the bell keeps working, while every notification that happens to land in a reconnect
	// gap is lost, forever, with nothing logged.
	//
	// Clearing the deadline outright fixes that but trades it for a worse failure: a peer
	// that vanished without closing (a laptop lid, a dropped VPN) leaves a write blocked
	// forever once the socket buffer fills, holding this goroutine and its subscriber slot
	// for the life of the process. So the deadline is PUSHED FORWARD before each write
	// instead — the stream lives as long as it is being consumed, and a peer that stopped
	// consuming fails a write and is reaped.
	rc := http.NewResponseController(w)
	extendDeadline := func() bool {
		// A ResponseWriter that cannot carry a deadline (an older or unwrapping middleware)
		// leaves the server's own timeout in force. The stream still works; it just ends
		// when that expires, which is the behaviour this had before.
		_ = rc.SetWriteDeadline(time.Now().Add(sseWriteTimeout))
		return true
	}
	extendDeadline()

	w.WriteHeader(http.StatusOK)
	// Prime the stream so proxies flush headers immediately.
	fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

	// A comment line on a timer. Nothing consumes it — its job is to make a dead peer
	// FAIL a write, so a client that vanished without closing (a laptop lid, a dropped
	// VPN) is reaped instead of holding a subscriber slot forever, and so intermediaries
	// that idle out a quiet connection see traffic.
	heartbeat := time.NewTicker(sseHeartbeatInterval)
	defer heartbeat.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			extendDeadline()
			if _, err := fmt.Fprintf(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case n, ok := <-ch:
			if !ok {
				return
			}
			payload, err := json.Marshal(n)
			if err != nil {
				continue
			}
			extendDeadline()
			if _, err := fmt.Fprintf(w, "event: notification\ndata: %s\n\n", payload); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// sseHeartbeatInterval is comfortably under the idle timeouts a proxy or load balancer
// typically applies (60s is common), while staying rare enough to be free.
const sseHeartbeatInterval = 20 * time.Second

// sseWriteTimeout bounds a SINGLE write to one client, refreshed before each. It must
// exceed the heartbeat interval — otherwise a healthy but quiet stream would expire
// between beats — while still being short enough that a peer which stopped reading is
// reaped in seconds rather than held for the life of the process.
const sseWriteTimeout = 2 * sseHeartbeatInterval

func (c *SSEChannel) subscribe() (int, chan Notification, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return 0, nil, false
	}
	id := c.nextID
	c.nextID++
	ch := make(chan Notification, c.clientBuf)
	c.clients[id] = ch
	return id, ch, true
}

func (c *SSEChannel) unsubscribe(id int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ch, ok := c.clients[id]; ok {
		delete(c.clients, id)
		close(ch)
	}
}

// Close disconnects all clients and rejects new connections.
func (c *SSEChannel) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	for id, ch := range c.clients {
		delete(c.clients, id)
		close(ch)
	}
	return nil
}

// ClientCount returns the number of connected clients (useful for diagnostics).
func (c *SSEChannel) ClientCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.clients)
}
