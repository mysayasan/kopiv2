package eventbus

import (
	"context"
	"sync"
	"testing"
	"time"
)

func collect(t *testing.T, bus Bus, ctx context.Context, topic string) (*[]string, *sync.Mutex) {
	t.Helper()
	var mu sync.Mutex
	got := []string{}
	if err := bus.Subscribe(ctx, topic, func(p []byte) {
		mu.Lock()
		got = append(got, string(p))
		mu.Unlock()
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	return &got, &mu
}

func waitFor(t *testing.T, mu *sync.Mutex, got *[]string, n int) []string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		if len(*got) >= n {
			out := append([]string(nil), *got...)
			mu.Unlock()
			return out
		}
		mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	out := append([]string(nil), *got...)
	mu.Unlock()
	t.Fatalf("timed out waiting for %d messages, got %v", n, out)
	return nil
}

// The single-instance case. Publisher and subscriber are the same process, so an
// in-process delivery reaches everyone there is to reach — this is the real implementation
// for a standalone install, not a stub.
func TestMemoryBusDeliversWithinTheProcess(t *testing.T) {
	bus := NewMemoryBus()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	got, mu := collect(t, bus, ctx, "t")
	if err := bus.Publish(ctx, "t", []byte("hello")); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if out := waitFor(t, mu, got, 1); out[0] != "hello" {
		t.Fatalf("got %q, want the published payload", out[0])
	}
}

// Every subscriber gets every message — this is a fan-out, not a work queue. A queue would
// hand each event to exactly one instance, which is the opposite of what the notification
// feed and the correlator need.
func TestMemoryBusFansOutToEverySubscriber(t *testing.T) {
	bus := NewMemoryBus()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a, amu := collect(t, bus, ctx, "t")
	b, bmu := collect(t, bus, ctx, "t")
	_ = bus.Publish(ctx, "t", []byte("x"))

	waitFor(t, amu, a, 1)
	waitFor(t, bmu, b, 1)
}

// Topics are isolated, so an unrelated subscription cannot see another feature's traffic.
func TestMemoryBusTopicsAreIsolated(t *testing.T) {
	bus := NewMemoryBus()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	other, omu := collect(t, bus, ctx, "other")
	want, wmu := collect(t, bus, ctx, "wanted")

	_ = bus.Publish(ctx, "wanted", []byte("x"))
	waitFor(t, wmu, want, 1)

	omu.Lock()
	n := len(*other)
	omu.Unlock()
	if n != 0 {
		t.Fatalf("a subscriber on another topic received %d messages", n)
	}
}

// Cancelling a subscription must actually stop delivery, or a restarted subscriber would
// accumulate duplicate handlers for the life of the process.
func TestMemoryBusUnsubscribesOnContextCancel(t *testing.T) {
	bus := NewMemoryBus()
	ctx, cancel := context.WithCancel(context.Background())
	got, mu := collect(t, bus, ctx, "t")

	_ = bus.Publish(context.Background(), "t", []byte("first"))
	waitFor(t, mu, got, 1)

	cancel()
	time.Sleep(50 * time.Millisecond) // let the unsubscribe land
	_ = bus.Publish(context.Background(), "t", []byte("second"))
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(*got) != 1 {
		t.Fatalf("got %d messages after cancelling, want 1", len(*got))
	}
}

// A slow subscriber must not stall the publisher: publishing happens on the node-event
// ingest path, and blocking it would let one wedged consumer back up the control channel.
func TestMemoryBusPublishDoesNotBlockOnSlowSubscriber(t *testing.T) {
	bus := NewMemoryBus()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	release := make(chan struct{})
	_ = bus.Subscribe(ctx, "t", func([]byte) { <-release })
	defer close(release)

	done := make(chan struct{})
	go func() { _ = bus.Publish(ctx, "t", []byte("x")); close(done) }()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Publish blocked on a slow subscriber")
	}
}

// The in-process provider must report itself as non-distributed, because callers skip
// cross-instance work (publishing, subscribing, echo suppression) on that answer.
func TestProviderSelection(t *testing.T) {
	for _, name := range []string{"", "default", "memory", "inmemory", "MEMORY"} {
		bus, resolved, err := New(Config{Provider: name})
		if err != nil {
			t.Fatalf("New(%q): %v", name, err)
		}
		if resolved != "memory" || bus.Distributed() {
			t.Fatalf("New(%q) = %q distributed=%v, want an in-process bus", name, resolved, bus.Distributed())
		}
	}
	// An unknown provider must fail loudly. Degrading to in-process would leave a
	// clustered deployment with a dark bell and rules that never fire, while looking
	// configured.
	if _, _, err := New(Config{Provider: "kafka"}); err == nil {
		t.Fatal("an unsupported provider must be an error, not a silent in-process fallback")
	}
}

func TestTopicKeyNamespacing(t *testing.T) {
	cases := []struct{ prefix, app, want string }{
		{"kopiv2", "myseliasan", "kopiv2:myseliasan:bus:node-events"},
		{"kopiv2", "", "kopiv2:bus:node-events"},
		{"", "myseliasan", "myseliasan:bus:node-events"},
		{"", "", "bus:node-events"},
	}
	for _, tc := range cases {
		if got := topicKey(tc.prefix, tc.app, "node-events"); got != tc.want {
			t.Fatalf("topicKey(%q, %q): got %q, want %q", tc.prefix, tc.app, got, tc.want)
		}
	}

	// The point of including the app: every app in the suite is configured with the SAME
	// KeyPrefix, so the prefix alone cannot keep two apps' topics apart. Without the app
	// segment these two would be one channel, and one app's events would be delivered to
	// the other's subscribers.
	a := topicKey("kopiv2", "myseliasan", "notifications")
	b := topicKey("kopiv2", "myidsan", "notifications")
	if a == b {
		t.Fatalf("two apps sharing one Redis collided on %q", a)
	}
}
