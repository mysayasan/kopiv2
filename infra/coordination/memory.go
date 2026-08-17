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

// TryLock takes the resource if it is free right now, and reports ErrNotAcquired
// otherwise. It never queues, so a caller that loses can get on with skipping.
//
// In a single process this is the whole of leader election: nobody else is running,
// so the one process always wins and behaves exactly as it did before any of this
// existed. That is why the memory provider needs no special case anywhere upstream.
func (m *MemoryLocker) TryLock(_ context.Context, resource string) (Lock, error) {
	start := time.Now()

	m.mu.Lock()
	queue := m.queue(resource)
	if queue.owner != "" {
		m.mu.Unlock()
		observe(m.recorder, telemetry.CoordinationMetric{
			AppName:  m.cfg.AppName,
			Provider: m.cfg.Provider,
			Resource: resource,
			Outcome:  "not-acquired",
			WaitMs:   time.Since(start).Milliseconds(),
		})
		return nil, ErrNotAcquired
	}
	token := uuid.NewString()
	queue.owner = token
	m.mu.Unlock()

	observe(m.recorder, telemetry.CoordinationMetric{
		AppName:  m.cfg.AppName,
		Provider: m.cfg.Provider,
		Resource: resource,
		Outcome:  "acquired",
		WaitMs:   time.Since(start).Milliseconds(),
	})
	// Deliberately no monitor(): the stuck-timeout metric describes work locks that
	// are held too long, and a lock taken through TryLock may be held for the life of
	// the process on purpose (a leadership lease). Reporting that as "stuck" every
	// StuckTimeout would be a false alarm on a healthy deployment.
	return &memoryLock{parent: m, resource: resource, token: token, done: make(chan struct{})}, nil
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

// Valid reports whether this in-process lock is still held. There is no lease to
// expire and no store to lose the key, so the only way to stop holding it is to
// release it.
func (l *memoryLock) Valid(_ context.Context) bool {
	select {
	case <-l.done:
		return false
	default:
	}
	l.parent.mu.Lock()
	defer l.parent.mu.Unlock()
	queue := l.parent.queues[l.resource]
	return queue != nil && queue.owner == l.token
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
