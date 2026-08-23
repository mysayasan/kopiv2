package fleetnode

import (
	"errors"
	"sync"
	"testing"

	"github.com/mysayasan/kopiv2/infra/control"
	"github.com/mysayasan/kopiv2/infra/telemetry"
)

// countingMetrics records every Inc so the tests can assert on labels, not just totals —
// the labels are the whole value of this counter (a drop is actionable only if you know
// which kind of event went missing and why).
type countingMetrics struct {
	telemetry.Metrics
	mu     sync.Mutex
	counts map[string]int
	// seen records every series that was ever touched, including the ones registered at
	// zero — which is the difference between "no drops" and "not instrumented".
	seen map[string]bool
}

func newCountingMetrics() *countingMetrics {
	return &countingMetrics{counts: map[string]int{}, seen: map[string]bool{}}
}

func (m *countingMetrics) exported(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.seen[key]
}

func (m *countingMetrics) Describe(string, string) {}

func (m *countingMetrics) Add(name string, labels telemetry.Labels, delta float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := m.key(name, labels)
	m.counts[key] += int(delta)
	m.seen[key] = true
}

func (m *countingMetrics) key(name string, labels telemetry.Labels) string {
	key := name
	if kind, ok := labels["kind"]; ok {
		key += "|kind=" + kind
	}
	if reason, ok := labels["reason"]; ok {
		key += "|reason=" + reason
	}
	return key
}

func (m *countingMetrics) Inc(name string, labels telemetry.Labels) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := m.key(name, labels)
	m.counts[key]++
	m.seen[key] = true
}

func (m *countingMetrics) get(key string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.counts[key]
}

// TestForwardEventCountsDropsWhenDisconnected is the defect this item exists for. The old
// code returned silently when the channel was down, so a node whose events were vanishing
// and a node with nothing to say produced identical telemetry — nothing.
func TestForwardEventCountsDropsWhenDisconnected(t *testing.T) {
	metrics := newCountingMetrics()
	m := &ControlChannelManager{logf: func(string, ...any) {}}
	m.SetMetrics(metrics)

	// No active connection: every forward is a loss.
	m.ForwardEvent("notification", []byte(`{}`))
	m.ForwardEvent("notification", []byte(`{}`))
	m.ForwardEvent("going-offline", nil)

	if got := m.DroppedSinceConnect(); got != 3 {
		t.Fatalf("droppedSinceConnect = %d, want 3", got)
	}
	if got := metrics.get(MetricControlEventsDropped + "|kind=notification|reason=disconnected"); got != 2 {
		t.Errorf("notification drops = %d, want 2", got)
	}
	if got := metrics.get(MetricControlEventsDropped + "|kind=going-offline|reason=disconnected"); got != 1 {
		t.Errorf("going-offline drops = %d, want 1", got)
	}
	// A drop must never be counted as a success — the ratio is the whole point.
	if got := metrics.get(MetricControlEventsForwarded + "|kind=notification"); got != 0 {
		t.Errorf("forwarded = %d while disconnected, want 0", got)
	}
}

// TestForwardEventWithoutMetricsStillCounts. The recorder is optional; the count that
// rides upstream on the next hello is not, or a node without telemetry would silently
// under-report its own losses to the control plane.
func TestForwardEventWithoutMetricsStillCounts(t *testing.T) {
	m := &ControlChannelManager{logf: func(string, ...any) {}}
	m.ForwardEvent("notification", []byte(`{}`))
	if got := m.DroppedSinceConnect(); got != 1 {
		t.Fatalf("droppedSinceConnect = %d with no metrics recorder, want 1", got)
	}
}

// TestDropCountIsConcurrencySafe. ForwardEvent runs on whichever goroutine published the
// notification, so the counter is genuinely contended on a busy node.
func TestDropCountIsConcurrencySafe(t *testing.T) {
	m := &ControlChannelManager{logf: func(string, ...any) {}}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				m.ForwardEvent("notification", nil)
			}
		}()
	}
	wg.Wait()
	if got := m.DroppedSinceConnect(); got != 1000 {
		t.Fatalf("droppedSinceConnect = %d, want 1000", got)
	}
}

// fakeConn is a control connection that either accepts frames or fails them.
type fakeConn struct {
	mu     sync.Mutex
	frames []*control.Frame
	err    error
}

func (c *fakeConn) WriteFrame(f *control.Frame) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return c.err
	}
	c.frames = append(c.frames, f)
	return nil
}

func (c *fakeConn) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.frames)
}

// TestForwardEventCountsSuccessesSeparately. A drop counter with no matching success
// counter is a number nobody can size — "14 dropped" means nothing without knowing whether
// the node forwarded fourteen or fourteen thousand.
func TestForwardEventCountsSuccessesSeparately(t *testing.T) {
	metrics := newCountingMetrics()
	m := &ControlChannelManager{logf: func(string, ...any) {}}
	m.SetMetrics(metrics)
	conn := &fakeConn{}
	m.setActive(conn)

	m.ForwardEvent("notification", []byte(`{}`))
	m.ForwardEvent("notification", []byte(`{}`))

	if conn.count() != 2 {
		t.Fatalf("frames written = %d, want 2", conn.count())
	}
	if got := metrics.get(MetricControlEventsForwarded + "|kind=notification"); got != 2 {
		t.Fatalf("forwarded = %d, want 2", got)
	}
	if got := m.DroppedSinceConnect(); got != 0 {
		t.Fatalf("droppedSinceConnect = %d after successful forwards, want 0", got)
	}
	if got := metrics.get(MetricControlEventsDropped + "|kind=notification|reason=disconnected"); got != 0 {
		t.Fatalf("a successful forward was counted as a drop (%d)", got)
	}
}

// TestForwardEventCountsAFailedWriteSeparately. The channel looked up and the write failed:
// the event is lost with the connection that was going away. Counted apart from
// "disconnected" because it means something different — the node believed it was connected,
// which is the case an operator would otherwise never suspect.
func TestForwardEventCountsAFailedWriteSeparately(t *testing.T) {
	metrics := newCountingMetrics()
	m := &ControlChannelManager{logf: func(string, ...any) {}}
	m.SetMetrics(metrics)
	m.setActive(&fakeConn{err: errWriteFailed})

	m.ForwardEvent("notification", []byte(`{}`))

	if got := m.DroppedSinceConnect(); got != 1 {
		t.Fatalf("droppedSinceConnect = %d after a failed write, want 1", got)
	}
	if got := metrics.get(MetricControlEventsDropped + "|kind=notification|reason=write_failed"); got != 1 {
		t.Fatalf("write_failed drops = %d, want 1", got)
	}
	if got := metrics.get(MetricControlEventsDropped + "|kind=notification|reason=disconnected"); got != 0 {
		t.Fatalf("a failed write was mislabelled as disconnected (%d)", got)
	}
	if got := metrics.get(MetricControlEventsForwarded + "|kind=notification"); got != 0 {
		t.Fatalf("a failed write was counted as a forward (%d)", got)
	}
}

var errWriteFailed = errors.New("connection reset")

// TestSetMetricsPublishesZeroSeries. A Prometheus counter with no samples is absent from
// the scrape entirely, so a node that has never dropped an event and a node with no
// instrumentation at all look identical — the very confusion this counter exists to end,
// reproduced one level up. A healthy node must export "0", not nothing.
func TestSetMetricsPublishesZeroSeries(t *testing.T) {
	metrics := newCountingMetrics()
	m := &ControlChannelManager{logf: func(string, ...any) {}}
	m.SetMetrics(metrics)

	for _, key := range []string{
		MetricControlEventsForwarded + "|kind=notification",
		MetricControlEventsDropped + "|kind=notification|reason=disconnected",
		MetricControlEventsDropped + "|kind=notification|reason=write_failed",
		MetricControlEventsDropped + "|kind=going-offline|reason=disconnected",
	} {
		if !metrics.exported(key) {
			t.Errorf("series %q is not exported before anything happens", key)
		}
		if got := metrics.counts[key]; got != 0 {
			t.Errorf("series %q starts at %d, want 0", key, got)
		}
	}
}
