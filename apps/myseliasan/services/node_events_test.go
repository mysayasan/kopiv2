package services

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/domain/notification"
	"github.com/mysayasan/kopiv2/infra/eventbus"
)

// distributedBus wraps the in-process bus but reports itself as distributed, so the
// publish/subscribe helpers take the cross-instance path they would take with Redis. That
// is what lets these tests exercise the real routing logic without a broker.
type distributedBus struct{ *eventbus.MemoryBus }

func (distributedBus) Distributed() bool { return true }

func newTestBus() eventbus.Bus { return distributedBus{eventbus.NewMemoryBus()} }

func collectEvents(t *testing.T, bus eventbus.Bus, ctx context.Context, instanceID string) (*[]NodeEventMessage, *sync.Mutex) {
	t.Helper()
	var mu sync.Mutex
	got := []NodeEventMessage{}
	SubscribeNodeEvents(ctx, bus, instanceID, func(m NodeEventMessage) {
		mu.Lock()
		got = append(got, m)
		mu.Unlock()
	}, nil)
	return &got, &mu
}

func waitEvents(t *testing.T, mu *sync.Mutex, got *[]NodeEventMessage, n int) []NodeEventMessage {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		if len(*got) >= n {
			out := append([]NodeEventMessage(nil), *got...)
			mu.Unlock()
			return out
		}
		mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d events", n)
	return nil
}

// The point of the node-event topic: an event that reached only instance A is delivered to
// instance B's correlator, carrying the RAW event. This is what lets a fleet rule whose
// conditions span nodes on different instances finally see them together.
func TestNodeEventReachesTheOtherInstance(t *testing.T) {
	bus := newTestBus()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	got, mu := collectEvents(t, bus, ctx, "instance-b")
	PublishNodeEvent(ctx, bus, "instance-a", "node-1", "notification", []byte(`{"title":"Door forced"}`), nil)

	out := waitEvents(t, mu, got, 1)
	if out[0].NodeID != "node-1" || out[0].Kind != "notification" {
		t.Fatalf("got %+v, want the origin's node id and kind", out[0])
	}
	// The raw event, not our own re-published notification: correlating on our own output
	// would let one rule's alert satisfy another rule's clause.
	if string(out[0].Body) != `{"title":"Door forced"}` {
		t.Fatalf("got body %q, want the node's raw event verbatim", out[0].Body)
	}
}

// An instance must ignore the echo of its own message. Without this every event would be
// handled twice at its origin: once inline and once off the bus — a duplicated bell entry
// and a double-fed correlator.
func TestNodeEventOriginIgnoresItsOwnEcho(t *testing.T) {
	bus := newTestBus()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	got, mu := collectEvents(t, bus, ctx, "instance-a")
	PublishNodeEvent(ctx, bus, "instance-a", "node-1", "notification", []byte(`{}`), nil)
	// Give it every chance to arrive wrongly.
	time.Sleep(150 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(*got) != 0 {
		t.Fatalf("an instance handled its own published event %d time(s)", len(*got))
	}
}

// The live feed's half. Every notification an instance publishes must reach the others,
// with the SAME id, so a browser sees one entry rather than a near-copy per instance.
func TestNotificationRelayReachesTheOtherInstance(t *testing.T) {
	bus := newTestBus()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	var got []notification.Notification
	SubscribeNotifications(ctx, bus, "instance-b", func(n notification.Notification) {
		mu.Lock()
		got = append(got, n)
		mu.Unlock()
	}, nil)

	ch := NewNotificationRelayChannel(bus, "instance-a", nil)
	if err := ch.Send(ctx, notification.Notification{ID: "42", Title: "Node lost"}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("got %d relayed notifications, want 1", len(got))
	}
	if got[0].ID != "42" || got[0].Title != "Node lost" {
		t.Fatalf("got %+v, want the row relayed with its assigned id", got[0])
	}
}

// The relay must cover notifications the control plane raises BY ITSELF — a node-lost
// alert, an anomaly, the daily digest — not just those arriving from a node. Wiring it to
// the node-event path only would have left every locally-generated alert invisible on the
// other instances, which is a silent failure of exactly the kind this phase exists to end.
func TestNotificationRelayCarriesLocallyGeneratedAlerts(t *testing.T) {
	bus := newTestBus()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	var got []notification.Notification
	SubscribeNotifications(ctx, bus, "instance-b", func(n notification.Notification) {
		mu.Lock()
		got = append(got, n)
		mu.Unlock()
	}, nil)

	ch := NewNotificationRelayChannel(bus, "instance-a", nil)
	// No node id anywhere: this is the heartbeat's own alert.
	_ = ch.Send(ctx, notification.Notification{ID: "7", Title: "Node lost", Source: "fleet-health"})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("a locally generated alert was not relayed to the other instances")
}

// The origin must ignore the echo of its own relayed notification, or it would push every
// notification to its own browsers twice.
func TestNotificationRelayIgnoresOwnEcho(t *testing.T) {
	bus := newTestBus()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	seen := 0
	SubscribeNotifications(ctx, bus, "instance-a", func(notification.Notification) {
		mu.Lock()
		seen++
		mu.Unlock()
	}, nil)

	_ = NewNotificationRelayChannel(bus, "instance-a", nil).Send(ctx, notification.Notification{ID: "1"})
	time.Sleep(150 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if seen != 0 {
		t.Fatalf("an instance relayed its own notification back to itself %d time(s)", seen)
	}
}

// A single instance must not put notifications on a bus nobody is listening to.
func TestNotificationRelayIsInertStandalone(t *testing.T) {
	plain := eventbus.NewMemoryBus() // Distributed() == false
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	seen := 0
	SubscribeNotifications(ctx, plain, "solo", func(notification.Notification) {
		mu.Lock()
		seen++
		mu.Unlock()
	}, nil)

	if err := NewNotificationRelayChannel(plain, "solo", nil).Send(ctx, notification.Notification{ID: "1"}); err != nil {
		t.Fatalf("Send on a single instance should be a no-op, got %v", err)
	}
	time.Sleep(120 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if seen != 0 {
		t.Fatalf("a single instance exchanged %d relay messages", seen)
	}
}

// A single instance has nobody to tell. Publishing would be a pointless round trip on the
// ingest path, and subscribing would deliver its own events back to it.
func TestStandaloneDoesNotUseTheBus(t *testing.T) {
	plain := eventbus.NewMemoryBus() // Distributed() == false
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	delivered := 0
	SubscribeNodeEvents(ctx, plain, "solo", func(NodeEventMessage) {
		mu.Lock()
		delivered++
		mu.Unlock()
	}, nil)

	PublishNodeEvent(ctx, plain, "solo", "node-1", "notification", []byte(`{}`), nil)
	time.Sleep(120 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if delivered != 0 {
		t.Fatalf("a single instance exchanged %d bus messages; it should exchange none", delivered)
	}
}

// Identity has to be stable within a process and distinct between instances, or echo
// suppression either drops other instances' events or fails to drop its own.
func TestInstanceIDIdentity(t *testing.T) {
	if got := NewInstanceID("https://a:3002"); got != "https://a:3002" {
		t.Fatalf("got %q, want the advertise URL used as identity", got)
	}
	a, b := NewInstanceID(""), NewInstanceID("")
	if a == "" || a == b {
		t.Fatalf("unconfigured instances must still get distinct identities; got %q and %q", a, b)
	}
}

// A malformed message must not take the subscriber down — one bad publisher (or a
// mid-rollout version skew) would otherwise stop every later event on that instance.
func TestMalformedBusMessageIsIgnored(t *testing.T) {
	bus := newTestBus()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	got, mu := collectEvents(t, bus, ctx, "instance-b")
	_ = bus.Publish(ctx, NodeEventTopic, []byte("{not json"))
	PublishNodeEvent(ctx, bus, "instance-a", "node-1", "health", []byte(`{}`), nil)

	out := waitEvents(t, mu, got, 1)
	if out[0].NodeID != "node-1" {
		t.Fatalf("the good event after a malformed one was not delivered; got %+v", out[0])
	}
}
