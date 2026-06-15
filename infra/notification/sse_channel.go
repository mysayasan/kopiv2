package notification

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
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
	w.WriteHeader(http.StatusOK)
	// Prime the stream so proxies flush headers immediately.
	fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case n, ok := <-ch:
			if !ok {
				return
			}
			payload, err := json.Marshal(n)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: notification\ndata: %s\n\n", payload)
			flusher.Flush()
		}
	}
}

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
