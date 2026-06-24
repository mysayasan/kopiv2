package notification

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// defaultRetryBackoffs is the schedule of waits between delivery attempts for an
// outbound channel. The first attempt is immediate; a failure then retries after
// each successive backoff before the notification is dropped. The schedule is
// deliberately short and front-loaded so a cold-start hiccup (DNS not ready yet,
// a broker still completing its first connect) recovers within a few seconds
// without stalling the single delivery worker for long. Total worst case ≈ 10s.
var defaultRetryBackoffs = []time.Duration{1 * time.Second, 3 * time.Second, 6 * time.Second}

// permanentError marks a delivery failure that retrying cannot fix (e.g. a
// payload that will not marshal), so the worker stops attempting immediately
// instead of burning the whole backoff schedule.
type permanentError struct{ err error }

func (e permanentError) Error() string { return e.err.Error() }

// permanent wraps err so the async worker treats it as non-retryable. A nil err
// stays nil.
func permanent(err error) error {
	if err == nil {
		return nil
	}
	return permanentError{err: err}
}

// asyncSender is a reusable background worker that decouples slow delivery
// (HTTP, bot APIs, ...) from the publisher. Outbound channels embed it and
// supply a deliver function; Send enqueues without blocking, and a single
// worker goroutine drains the queue. It also applies a per-channel minimum
// severity so low-priority notifications can be filtered close to the sink.
//
// A transient delivery failure (deliver returns a non-permanent error) is
// retried with backoff before the notification is dropped, so a notification
// fired before the network/broker is ready at cold start is not silently lost.
type asyncSender struct {
	name     string
	minSev   Severity
	queue    chan Notification
	wg       sync.WaitGroup
	closed   chan struct{}
	once     sync.Once
	deliver  func(Notification) error
	backoffs []time.Duration
	logger   Logger
}

func newAsyncSender(name string, minSeverity Severity, queueSize int, deliver func(Notification) error, logger Logger) *asyncSender {
	if queueSize <= 0 {
		queueSize = 256
	}
	s := &asyncSender{
		name:     name,
		minSev:   minSeverity,
		queue:    make(chan Notification, queueSize),
		closed:   make(chan struct{}),
		deliver:  deliver,
		backoffs: defaultRetryBackoffs,
		logger:   logger,
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
		s.deliverWithRetry(n)
	}
}

// deliverWithRetry attempts delivery, retrying transient failures on the backoff
// schedule. It stops early on success, on a permanentError, or once the channel
// is closing. The final failure is logged so the operator sees the dropped event.
func (s *asyncSender) deliverWithRetry(n Notification) {
	err := s.deliver(n)
	if err == nil {
		return
	}
	for attempt, backoff := range s.backoffs {
		if _, permanent := err.(permanentError); permanent {
			break
		}
		select {
		case <-s.closed:
			// Shutting down: give up the remaining retries rather than block Close.
			warn(s.logger, "notification."+s.name, "delivery abandoned on shutdown after %d attempt(s): %v", attempt+1, err)
			return
		case <-time.After(backoff):
		}
		if err = s.deliver(n); err == nil {
			return
		}
	}
	warn(s.logger, "notification."+s.name, "delivery failed after %d attempt(s), dropping notification %s: %v", len(s.backoffs)+1, n.ID, err)
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
