package notification

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log"
	"sync"
	"time"
)

// Hub fans one published notification out to every registered channel whose
// filter accepts it. Delivery is synchronous and isolated: a channel error or
// panic never blocks or aborts delivery to the other channels. Channels that
// perform slow I/O are expected to queue internally so Publish stays fast.
//
// Hub is safe for concurrent use.
type Hub struct {
	mu     sync.RWMutex
	subs   []subscription
	logger Logger
	now    func() time.Time
}

type subscription struct {
	channel Channel
	filter  Filter
}

// Option configures a Hub.
type Option func(*Hub)

// WithLogger sets the logger used for channel delivery errors.
func WithLogger(l Logger) Option {
	return func(h *Hub) { h.logger = l }
}

// NewHub creates an empty hub. Register channels before publishing.
func NewHub(opts ...Option) *Hub {
	h := &Hub{now: time.Now}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Register adds a channel with an optional filter. At most one filter is used;
// passing none means the channel receives every notification.
func (h *Hub) Register(channel Channel, filters ...Filter) {
	if channel == nil {
		return
	}
	var filter Filter
	if len(filters) > 0 {
		filter = filters[0]
	}
	h.mu.Lock()
	h.subs = append(h.subs, subscription{channel: channel, filter: filter})
	h.mu.Unlock()
}

// Publish normalizes the notification (assigning an ID, timestamp, and default
// severity when absent) and delivers it to every matching channel. It returns
// the normalized notification so callers can read the assigned ID.
func (h *Hub) Publish(ctx context.Context, n Notification) Notification {
	if n.ID == "" {
		n.ID = newID()
	}
	if n.CreatedAt == 0 {
		n.CreatedAt = h.now().UTC().Unix()
	}
	n.Severity = n.Severity.Normalize()

	h.mu.RLock()
	subs := make([]subscription, len(h.subs))
	copy(subs, h.subs)
	h.mu.RUnlock()

	for _, sub := range subs {
		if !sub.filter.Allows(n) {
			continue
		}
		h.deliver(ctx, sub.channel, n)
	}
	return n
}

// deliver sends to one channel, recovering from panics so a misbehaving channel
// cannot take down the publisher.
func (h *Hub) deliver(ctx context.Context, channel Channel, n Notification) {
	defer func() {
		if r := recover(); r != nil {
			h.logf("error", "notification.hub", "channel %q panicked: %v", channel.Name(), r)
		}
	}()
	if err := channel.Send(ctx, n); err != nil {
		h.logf("warn", "notification.hub", "channel %q send failed: %v", channel.Name(), err)
	}
}

// Close closes every registered channel that implements io.Closer, so internal
// queues are drained. It is safe to call once during shutdown.
func (h *Hub) Close(ctx context.Context) error {
	h.mu.Lock()
	subs := h.subs
	h.subs = nil
	h.mu.Unlock()

	var firstErr error
	for _, sub := range subs {
		closer, ok := sub.channel.(io.Closer)
		if !ok {
			continue
		}
		if err := closer.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (h *Hub) logf(level, source, format string, args ...any) {
	if h.logger == nil {
		log.Printf("["+level+"] "+source+": "+format, args...)
		return
	}
	switch level {
	case "error":
		h.logger.Errorf(source, format, args...)
	case "warn":
		h.logger.Warnf(source, format, args...)
	default:
		h.logger.Infof(source, format, args...)
	}
}

// newID returns a short random hex identifier. crypto/rand failure falls back to
// a timestamp so an ID is always produced.
func newID() string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "n" + hex.EncodeToString([]byte(time.Now().UTC().Format("20060102150405.000000000")))
	}
	return hex.EncodeToString(buf)
}
