package eventbus

import (
	"context"
	"sync"
)

// MemoryBus fans out within one process.
//
// This is the correct implementation for a single instance, not a stub: publisher and
// subscriber ARE the same process, so an in-process delivery reaches everyone there is to
// reach. It is also what keeps standalone installs free of any configuration — the same
// wiring runs, and Distributed() reports false so callers can skip the parts that only
// matter when other instances exist.
type MemoryBus struct {
	mu     sync.RWMutex
	subs   map[string][]*memorySub
	closed bool
}

type memorySub struct {
	handler Handler
	done    chan struct{}
}

func NewMemoryBus() *MemoryBus {
	return &MemoryBus{subs: map[string][]*memorySub{}}
}

func (m *MemoryBus) Distributed() bool { return false }

func (m *MemoryBus) Publish(_ context.Context, topic string, payload []byte) error {
	m.mu.RLock()
	subs := append([]*memorySub(nil), m.subs[topic]...)
	closed := m.closed
	m.mu.RUnlock()
	if closed {
		return nil
	}
	// Each handler runs on its own goroutine, matching the Redis provider's contract that
	// Publish never blocks on delivery. A caller must not be able to stall an ingest path
	// because one subscriber is slow.
	for _, s := range subs {
		go func(s *memorySub) {
			select {
			case <-s.done:
				return
			default:
			}
			// A copy per handler: subscribers must not be able to corrupt each other's view
			// (or the publisher's buffer) by retaining or mutating the slice.
			buf := append([]byte(nil), payload...)
			s.handler(buf)
		}(s)
	}
	return nil
}

func (m *MemoryBus) Subscribe(ctx context.Context, topic string, handler Handler) error {
	if handler == nil {
		return nil
	}
	sub := &memorySub{handler: handler, done: make(chan struct{})}

	m.mu.Lock()
	m.subs[topic] = append(m.subs[topic], sub)
	m.mu.Unlock()

	go func() {
		<-ctx.Done()
		close(sub.done)
		m.mu.Lock()
		rest := m.subs[topic][:0]
		for _, s := range m.subs[topic] {
			if s != sub {
				rest = append(rest, s)
			}
		}
		m.subs[topic] = rest
		m.mu.Unlock()
	}()
	return nil
}

func (m *MemoryBus) Ping(context.Context) error { return nil }

func (m *MemoryBus) Close() error {
	m.mu.Lock()
	m.closed = true
	m.subs = map[string][]*memorySub{}
	m.mu.Unlock()
	return nil
}
