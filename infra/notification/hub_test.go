package notification

import (
	"context"
	"sync"
	"testing"
)

type fakeChannel struct {
	mu       sync.Mutex
	received []Notification
	err      error
	panicMsg string
}

func (f *fakeChannel) Name() string { return "fake" }

func (f *fakeChannel) Send(_ context.Context, n Notification) error {
	if f.panicMsg != "" {
		panic(f.panicMsg)
	}
	f.mu.Lock()
	f.received = append(f.received, n)
	f.mu.Unlock()
	return f.err
}

func (f *fakeChannel) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.received)
}

func TestHubPublishFansOutAndAssignsIdAndTimestamp(t *testing.T) {
	a, b := &fakeChannel{}, &fakeChannel{}
	hub := NewHub()
	hub.Register(a)
	hub.Register(b)

	got := hub.Publish(context.Background(), Notification{Title: "hi"})

	if got.ID == "" {
		t.Fatal("expected hub to assign an ID")
	}
	if got.CreatedAt == 0 {
		t.Fatal("expected hub to assign CreatedAt")
	}
	if got.Severity != Info {
		t.Fatalf("expected default severity Info, got %q", got.Severity)
	}
	if a.count() != 1 || b.count() != 1 {
		t.Fatalf("expected both channels to receive 1, got a=%d b=%d", a.count(), b.count())
	}
}

func TestHubFilterByCategoryAndSeverity(t *testing.T) {
	ch := &fakeChannel{}
	hub := NewHub()
	hub.Register(ch, Filter{MinSeverity: Warning, Categories: []string{CategoryVisionAlert}})

	hub.Publish(context.Background(), Notification{Category: CategoryVisionAlert, Severity: Info})     // below floor
	hub.Publish(context.Background(), Notification{Category: CategoryHealthCheck, Severity: Critical}) // wrong category
	hub.Publish(context.Background(), Notification{Category: CategoryVisionAlert, Severity: Critical}) // matches

	if ch.count() != 1 {
		t.Fatalf("expected exactly 1 delivered notification, got %d", ch.count())
	}
}

func TestHubChannelErrorDoesNotBlockOthers(t *testing.T) {
	bad := &fakeChannel{err: context.DeadlineExceeded}
	good := &fakeChannel{}
	hub := NewHub()
	hub.Register(bad)
	hub.Register(good)

	hub.Publish(context.Background(), Notification{Title: "x"})

	if good.count() != 1 {
		t.Fatalf("expected good channel to still receive, got %d", good.count())
	}
}

func TestHubChannelPanicIsContained(t *testing.T) {
	panicy := &fakeChannel{panicMsg: "boom"}
	good := &fakeChannel{}
	hub := NewHub()
	hub.Register(panicy)
	hub.Register(good)

	// Must not panic out of Publish.
	hub.Publish(context.Background(), Notification{Title: "x"})

	if good.count() != 1 {
		t.Fatalf("expected good channel to receive after panic in another, got %d", good.count())
	}
}

func TestFilterAllowsZeroValue(t *testing.T) {
	var f Filter
	if !f.Allows(Notification{Category: "anything", Severity: Info}) {
		t.Fatal("zero-value filter should allow everything")
	}
}
