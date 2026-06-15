package notification

import (
	"context"
	"fmt"
	"sync"
)

// asyncSender is a reusable background worker that decouples slow delivery
// (HTTP, bot APIs, ...) from the publisher. Outbound channels embed it and
// supply a deliver function; Send enqueues without blocking, and a single
// worker goroutine drains the queue. It also applies a per-channel minimum
// severity so low-priority notifications can be filtered close to the sink.
type asyncSender struct {
	name    string
	minSev  Severity
	queue   chan Notification
	wg      sync.WaitGroup
	closed  chan struct{}
	once    sync.Once
	deliver func(Notification)
}

func newAsyncSender(name string, minSeverity Severity, queueSize int, deliver func(Notification)) *asyncSender {
	if queueSize <= 0 {
		queueSize = 256
	}
	s := &asyncSender{
		name:    name,
		minSev:  minSeverity,
		queue:   make(chan Notification, queueSize),
		closed:  make(chan struct{}),
		deliver: deliver,
	}
	s.wg.Add(1)
	go s.run()
	return s
}

func (s *asyncSender) Name() string { return s.name }

// Send enqueues without blocking. Notifications below the channel's minimum
// severity are skipped; a full queue drops the notification (returned as an
// error the hub logs) rather than stalling the publisher.
func (s *asyncSender) Send(_ context.Context, n Notification) error {
	if s.minSev != "" && n.Severity.rank() < s.minSev.rank() {
		return nil
	}
	select {
	case s.queue <- n:
		return nil
	case <-s.closed:
		return nil
	default:
		return fmt.Errorf("%s queue full; dropped notification %s", s.name, n.ID)
	}
}

func (s *asyncSender) run() {
	defer s.wg.Done()
	for n := range s.queue {
		s.deliver(n)
	}
}

// Close stops accepting notifications and waits for the queue to drain.
func (s *asyncSender) Close() error {
	s.once.Do(func() {
		close(s.closed)
		close(s.queue)
	})
	s.wg.Wait()
	return nil
}
