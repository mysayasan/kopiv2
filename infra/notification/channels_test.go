package notification

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSSEChannelDeliversToSubscriber(t *testing.T) {
	sse := NewSSEChannel(SSEOptions{ClientBuffer: 4})
	id, ch, ok := sse.subscribe()
	if !ok {
		t.Fatal("subscribe failed")
	}
	defer sse.unsubscribe(id)
	if sse.ClientCount() != 1 {
		t.Fatalf("ClientCount = %d, want 1", sse.ClientCount())
	}

	sse.Send(context.Background(), Notification{ID: "abc"})

	select {
	case n := <-ch:
		if n.ID != "abc" {
			t.Fatalf("delivered ID = %q, want abc", n.ID)
		}
	default:
		t.Fatal("expected notification in client buffer")
	}
}

func TestSSEChannelDropsWhenClientBufferFull(t *testing.T) {
	sse := NewSSEChannel(SSEOptions{ClientBuffer: 1})
	id, ch, _ := sse.subscribe()
	defer sse.unsubscribe(id)

	sse.Send(context.Background(), Notification{ID: "1"})
	sse.Send(context.Background(), Notification{ID: "2"}) // buffer full -> dropped

	if len(ch) != 1 {
		t.Fatalf("buffered = %d, want 1 (second dropped)", len(ch))
	}
}

func TestSSEChannelCloseRejectsNewSubscribers(t *testing.T) {
	sse := NewSSEChannel(SSEOptions{})
	if err := sse.Close(); err != nil {
		t.Fatalf("Close returned %v", err)
	}
	if _, _, ok := sse.subscribe(); ok {
		t.Fatal("subscribe should fail after Close")
	}
}

func TestWebhookChannelDeliversAndDrainsOnClose(t *testing.T) {
	delivered := make(chan Notification, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var n Notification
		_ = json.NewDecoder(r.Body).Decode(&n)
		delivered <- n
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ch := NewWebhookChannel(WebhookOptions{URL: srv.URL})
	if err := ch.Send(context.Background(), Notification{ID: "xyz"}); err != nil {
		t.Fatalf("Send returned %v", err)
	}
	// Close drains the queue, guaranteeing delivery completed.
	if c, ok := ch.(io.Closer); ok {
		if err := c.Close(); err != nil {
			t.Fatalf("Close returned %v", err)
		}
	}

	select {
	case n := <-delivered:
		if n.ID != "xyz" {
			t.Fatalf("delivered ID = %q, want xyz", n.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("webhook notification was not delivered")
	}
}

func TestWebhookChannelDisabledWithoutURL(t *testing.T) {
	ch := NewWebhookChannel(WebhookOptions{})
	if err := ch.Send(context.Background(), Notification{ID: "1"}); err != nil {
		t.Fatalf("disabled webhook should no-op, got %v", err)
	}
	if c, ok := ch.(io.Closer); ok {
		if err := c.Close(); err != nil {
			t.Fatalf("Close on disabled webhook returned %v", err)
		}
	}
}

func TestLogChannelNeverErrors(t *testing.T) {
	ch := NewLogChannel(nil) // falls back to std logger
	for _, sev := range []Severity{Info, Warning, Critical, Severity("unknown")} {
		if err := ch.Send(context.Background(), Notification{Severity: sev, Title: "t"}); err != nil {
			t.Fatalf("log channel returned error for severity %q: %v", sev, err)
		}
	}
}
