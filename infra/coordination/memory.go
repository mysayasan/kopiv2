package coordination

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mysayasan/kopiv2/infra/telemetry"
)

type MemoryLocker struct {
	mu       sync.Mutex
	cfg      Config
	recorder recorder
	queues   map[string]*memoryQueue
}

type memoryQueue struct {
	owner   string
	waiters []string
}

type memoryLock struct {
	parent   *MemoryLocker
	resource string
	token    string
	done     chan struct{}
	once     sync.Once
}

func NewMemoryLocker(cfg Config, rec recorder) *MemoryLocker {
	cfg = normalizeConfig(cfg)
	if cfg.Provider == "" {
		cfg.Provider = "memory"
	}
	return &MemoryLocker{
		cfg:      cfg,
		recorder: rec,
		queues:   map[string]*memoryQueue{},
	}
}

func (m *MemoryLocker) Lock(ctx context.Context, resource string) (Lock, error) {
	start := time.Now()
	token := uuid.NewString()
	waitCtx, cancel := context.WithTimeout(ctx, m.cfg.WaitTimeout)
	defer cancel()

	m.mu.Lock()
	queue := m.queue(resource)
	queue.waiters = append(queue.waiters, token)
	m.mu.Unlock()

	ticker := time.NewTicker(m.cfg.PollInterval)
	defer ticker.Stop()

	for {
		m.mu.Lock()
		queue = m.queue(resource)
		if queue.owner == "" && len(queue.waiters) > 0 && queue.waiters[0] == token {
			queue.waiters = queue.waiters[1:]
			queue.owner = token
			m.mu.Unlock()

			lock := &memoryLock{
				parent:   m,
				resource: resource,
				token:    token,
				done:     make(chan struct{}),
			}
			lock.monitor()
			observe(m.recorder, telemetry.CoordinationMetric{
				AppName:  m.cfg.AppName,
				Provider: m.cfg.Provider,
				Resource: resource,
				Outcome:  "acquired",
				WaitMs:   time.Since(start).Milliseconds(),
			})
			return lock, nil
		}
		m.mu.Unlock()

		select {
		case <-waitCtx.Done():
			m.removeWaiter(resource, token)
			outcome := "timeout"
			if ctx.Err() != nil {
				outcome = "canceled"
			}
			observe(m.recorder, telemetry.CoordinationMetric{
				AppName:  m.cfg.AppName,
				Provider: m.cfg.Provider,
				Resource: resource,
				Outcome:  outcome,
				WaitMs:   time.Since(start).Milliseconds(),
			})
			return nil, waitCtx.Err()
		case <-ticker.C:
		}
	}
}

func (m *MemoryLocker) Close() error {
	return nil
}

func (m *MemoryLocker) Ping(context.Context) error {
	return nil
}

func (m *MemoryLocker) queue(resource string) *memoryQueue {
	queue := m.queues[resource]
	if queue == nil {
		queue = &memoryQueue{}
		m.queues[resource] = queue
	}
	return queue
}

func (m *MemoryLocker) removeWaiter(resource string, token string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	queue := m.queue(resource)
	for i, waiter := range queue.waiters {
		if waiter == token {
			queue.waiters = append(queue.waiters[:i], queue.waiters[i+1:]...)
			break
		}
	}
}

func (l *memoryLock) Resource() string {
	return l.resource
}

func (l *memoryLock) Token() string {
	return l.token
}

func (l *memoryLock) Release(_ context.Context) error {
	l.once.Do(func() {
		close(l.done)
		l.parent.mu.Lock()
		defer l.parent.mu.Unlock()

		queue := l.parent.queue(l.resource)
		if queue.owner == l.token {
			queue.owner = ""
		}
		if queue.owner == "" && len(queue.waiters) == 0 {
			delete(l.parent.queues, l.resource)
		}
	})
	return nil
}

func (l *memoryLock) monitor() {
	timeout := l.parent.cfg.StuckTimeout
	if timeout <= 0 {
		return
	}
	go func() {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case <-timer.C:
			observe(l.parent.recorder, telemetry.CoordinationMetric{
				AppName:  l.parent.cfg.AppName,
				Provider: l.parent.cfg.Provider,
				Resource: l.resource,
				Outcome:  "stuck",
				WaitMs:   timeout.Milliseconds(),
			})
		case <-l.done:
		}
	}()
}
