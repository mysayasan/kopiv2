package notification

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTelegramChannelDeliversMessage(t *testing.T) {
	received := make(chan telegramMessage, 1)
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		var msg telegramMessage
		_ = json.NewDecoder(r.Body).Decode(&msg)
		received <- msg
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	ch := NewTelegramChannel(TelegramOptions{
		BotToken:   "123:ABC",
		ChatID:     "42",
		APIBaseURL: srv.URL,
	})
	if err := ch.Send(context.Background(), Notification{Severity: Critical, Title: "Intrusion", Body: "Camera 1"}); err != nil {
		t.Fatalf("Send returned %v", err)
	}
	if c, ok := ch.(io.Closer); ok {
		_ = c.Close()
	}

	select {
	case msg := <-received:
		if msg.ChatID != "42" {
			t.Errorf("chat_id = %q, want 42", msg.ChatID)
		}
		if !strings.Contains(msg.Text, "Intrusion") || !strings.Contains(msg.Text, "Camera 1") {
			t.Errorf("text missing content: %q", msg.Text)
		}
		if !strings.HasSuffix(gotPath, "/bot123:ABC/sendMessage") {
			t.Errorf("unexpected path %q", gotPath)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("telegram message not delivered")
	}
}

func TestTelegramChannelDisabledWithoutCredentials(t *testing.T) {
	for _, opts := range []TelegramOptions{
		{BotToken: "", ChatID: "42"},
		{BotToken: "123:ABC", ChatID: ""},
	} {
		ch := NewTelegramChannel(opts)
		if err := ch.Send(context.Background(), Notification{Title: "x"}); err != nil {
			t.Fatalf("disabled telegram should no-op, got %v", err)
		}
	}
}

func TestTelegramChannelRespectsMinSeverity(t *testing.T) {
	var count int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ch := NewTelegramChannel(TelegramOptions{
		BotToken:    "123:ABC",
		ChatID:      "42",
		APIBaseURL:  srv.URL,
		MinSeverity: Critical,
	})
	_ = ch.Send(context.Background(), Notification{Severity: Info, Title: "low"})      // dropped
	_ = ch.Send(context.Background(), Notification{Severity: Critical, Title: "high"}) // sent
	if c, ok := ch.(io.Closer); ok {
		_ = c.Close()
	}
	if count != 1 {
		t.Fatalf("expected 1 delivery (min severity critical), got %d", count)
	}
}

func TestHTMLEscape(t *testing.T) {
	if got := htmlEscape("a<b>&c"); got != "a&lt;b&gt;&amp;c" {
		t.Fatalf("htmlEscape = %q", got)
	}
}

func TestReloadableChannelSwapsChildren(t *testing.T) {
	rc := NewReloadableChannel("outbound")
	a, b := &fakeChannel{}, &fakeChannel{}

	rc.Set([]Channel{a})
	_ = rc.Send(context.Background(), Notification{ID: "1"})
	if a.count() != 1 {
		t.Fatalf("first child should receive 1, got %d", a.count())
	}

	rc.Set([]Channel{b})
	_ = rc.Send(context.Background(), Notification{ID: "2"})
	if a.count() != 1 {
		t.Errorf("old child should not receive after swap, got %d", a.count())
	}
	if b.count() != 1 {
		t.Errorf("new child should receive 1, got %d", b.count())
	}

	_ = rc.Close()
	_ = rc.Send(context.Background(), Notification{ID: "3"})
	if b.count() != 1 {
		t.Errorf("no child should receive after close, got %d", b.count())
	}
}
