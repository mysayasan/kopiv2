package notification

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/infra/mailer"
)

// recordingSender is a MailSender that records what it was asked to send and
// returns a scripted result per call.
type recordingSender struct {
	mu       sync.Mutex
	sent     []mailer.Message
	results  []error
	attempts int
}

func (s *recordingSender) SendMessage(msg mailer.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, msg)
	i := s.attempts
	s.attempts++
	if i < len(s.results) {
		return s.results[i]
	}
	return nil
}

func (s *recordingSender) snapshot() []mailer.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]mailer.Message, len(s.sent))
	copy(out, s.sent)
	return out
}

// waitFor polls cond until it holds or the deadline passes. Delivery is async, so
// a bare assertion after Send races the worker.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestMailChannelSendsNotification(t *testing.T) {
	s := &recordingSender{}
	ch := NewMailChannel(MailOptions{
		Sender:          s,
		To:              []string{"ops@corp.test"},
		SubjectPrefix:   "[Site A]",
		IncludeSnapshot: true,
	})
	defer ch.(*asyncSender).Close()

	if err := ch.Send(context.Background(), Notification{
		ID: "n1", Category: CategoryVisionAlert, Severity: Critical,
		Title: "Person in restricted zone", Body: "Camera 3, Loading Bay",
		Source: "vision", Link: "https://nvr.corp.test/alerts/7", CreatedAt: 1755950000,
		Attachment: &Attachment{Filename: "alert-7.jpg", ContentType: "image/jpeg", Data: []byte{0xFF, 0xD8, 0xFF}},
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	waitFor(t, "the message to be sent", func() bool { return len(s.snapshot()) == 1 })
	msg := s.snapshot()[0]

	if msg.Subject != "[Site A] [CRITICAL] Person in restricted zone" {
		t.Errorf("subject = %q", msg.Subject)
	}
	if strings.Join(msg.To, ",") != "ops@corp.test" {
		t.Errorf("recipients = %v", msg.To)
	}
	for _, want := range []string{"Camera 3, Loading Bay", "Severity: critical", "vision.alert", "https://nvr.corp.test/alerts/7"} {
		if !strings.Contains(msg.Body, want) {
			t.Errorf("body missing %q:\n%s", want, msg.Body)
		}
	}
	// The timestamp must be labelled UTC — an unlabelled local time in an email
	// read on another continent is worse than no timestamp at all.
	if !strings.Contains(msg.Body, "UTC") {
		t.Errorf("timestamp not labelled UTC:\n%s", msg.Body)
	}
	if msg.Headers["X-Kopiv2-Severity"] != "critical" || msg.Headers["X-Kopiv2-Category"] != CategoryVisionAlert {
		t.Errorf("classification headers = %v", msg.Headers)
	}
	if len(msg.Attachments) != 1 || msg.Attachments[0].Filename != "alert-7.jpg" {
		t.Errorf("snapshot not attached: %+v", msg.Attachments)
	}
}

// TestMailChannelOmitsSnapshotWhenDisabled asserts the per-destination toggle is
// honoured: a recipient on a metered link asked not to receive images.
func TestMailChannelOmitsSnapshotWhenDisabled(t *testing.T) {
	s := &recordingSender{}
	ch := NewMailChannel(MailOptions{Sender: s, To: []string{"ops@corp.test"}, IncludeSnapshot: false})
	defer ch.(*asyncSender).Close()

	_ = ch.Send(context.Background(), Notification{
		ID: "n1", Title: "Alert", Severity: Info,
		Attachment: &Attachment{Filename: "a.jpg", ContentType: "image/jpeg", Data: []byte{1, 2, 3}},
	})
	waitFor(t, "the message to be sent", func() bool { return len(s.snapshot()) == 1 })
	if len(s.snapshot()[0].Attachments) != 0 {
		t.Error("snapshot attached even though the destination disabled it")
	}
}

// TestMailChannelPartialRejectionIsNotRetried is the assertion that keeps a
// distribution list with one dead address from mailing everyone else four copies
// of every alert. A partial rejection is a DELIVERY, so the worker must stop.
func TestMailChannelPartialRejectionIsNotRetried(t *testing.T) {
	s := &recordingSender{results: []error{
		&mailer.RecipientError{Rejected: []mailer.RejectedRecipient{{Address: "gone@corp.test", Err: errors.New("550 user unknown")}}},
	}}
	ch := NewMailChannel(MailOptions{Sender: s, To: []string{"gone@corp.test", "ops@corp.test"}})
	defer ch.(*asyncSender).Close()

	_ = ch.Send(context.Background(), Notification{ID: "n1", Title: "Alert", Severity: Warning})
	waitFor(t, "the first attempt", func() bool { return len(s.snapshot()) >= 1 })

	// The retry schedule's first backoff is 1s; give it comfortably longer than
	// that to prove no second attempt is made.
	time.Sleep(1500 * time.Millisecond)
	if n := len(s.snapshot()); n != 1 {
		t.Errorf("a partially-rejected send was retried: %d attempts, want 1", n)
	}
}

// TestMailChannelTotalRejectionIsNotRetried asserts the complementary case.
// Nobody received it, but the relay refused the addresses BY NAME — retrying the
// same list cannot change that answer, and doing so burns the delivery worker.
func TestMailChannelTotalRejectionIsNotRetried(t *testing.T) {
	s := &recordingSender{results: []error{
		&mailer.RecipientError{AllRejected: true, Rejected: []mailer.RejectedRecipient{{Address: "gone@corp.test", Err: errors.New("550")}}},
	}}
	ch := NewMailChannel(MailOptions{Sender: s, To: []string{"gone@corp.test"}})
	defer ch.(*asyncSender).Close()

	_ = ch.Send(context.Background(), Notification{ID: "n1", Title: "Alert", Severity: Warning})
	waitFor(t, "the first attempt", func() bool { return len(s.snapshot()) >= 1 })
	time.Sleep(1500 * time.Millisecond)
	if n := len(s.snapshot()); n != 1 {
		t.Errorf("a name-rejected send was retried: %d attempts, want 1", n)
	}
}

// TestMailChannelRetriesTransientFailure asserts the OTHER side of the contract:
// a relay that is merely down IS retried, so an alert raised while the mail
// server was restarting is not lost.
func TestMailChannelRetriesTransientFailure(t *testing.T) {
	s := &recordingSender{results: []error{errors.New("dial tcp: connection refused")}}
	ch := NewMailChannel(MailOptions{Sender: s, To: []string{"ops@corp.test"}})
	defer ch.(*asyncSender).Close()

	_ = ch.Send(context.Background(), Notification{ID: "n1", Title: "Alert", Severity: Warning})
	waitFor(t, "the retry", func() bool { return len(s.snapshot()) >= 2 })
}

// TestMailChannelDisabledIsNoop asserts an unconfigured relay or an empty
// recipient list yields a channel that accepts and drops, so it can always be
// registered and switched on later by configuration.
func TestMailChannelDisabledIsNoop(t *testing.T) {
	cases := map[string]MailOptions{
		"no recipients":   {Sender: &recordingSender{}, To: nil},
		"blank recipient": {Sender: &recordingSender{}, To: []string{"  ", ""}},
		"relay disabled":  {Relay: mailer.Config{Enabled: false, Host: "relay.corp.test"}, To: []string{"ops@corp.test"}},
		"relay hostless":  {Relay: mailer.Config{Enabled: true, Host: ""}, To: []string{"ops@corp.test"}},
	}
	for name, opts := range cases {
		t.Run(name, func(t *testing.T) {
			ch := NewMailChannel(opts)
			if _, ok := ch.(noopChannel); !ok {
				t.Fatalf("expected a no-op channel, got %T", ch)
			}
			if err := ch.Send(context.Background(), Notification{Title: "x"}); err != nil {
				t.Errorf("no-op channel returned an error: %v", err)
			}
		})
	}
}

// TestMailChannelSeverityFloor asserts the per-destination floor is applied, so a
// mailbox subscribed to critical alerts is not filled with informational noise.
func TestMailChannelSeverityFloor(t *testing.T) {
	s := &recordingSender{}
	ch := NewMailChannel(MailOptions{Sender: s, To: []string{"ops@corp.test"}, MinSeverity: Critical})
	defer ch.(*asyncSender).Close()

	_ = ch.Send(context.Background(), Notification{ID: "n1", Title: "Chatter", Severity: Info})
	_ = ch.Send(context.Background(), Notification{ID: "n2", Title: "Intruder", Severity: Critical})

	waitFor(t, "the critical alert", func() bool { return len(s.snapshot()) == 1 })
	time.Sleep(200 * time.Millisecond)
	sent := s.snapshot()
	if len(sent) != 1 || !strings.Contains(sent[0].Subject, "Intruder") {
		t.Errorf("severity floor not applied; sent %d: %+v", len(sent), sent)
	}
}

// TestMailChannelSubjectFallback keeps a titleless notification from arriving as
// a blank-subject email that most clients render as unreadable.
func TestMailChannelSubjectFallback(t *testing.T) {
	s := &recordingSender{}
	ch := NewMailChannel(MailOptions{Sender: s, To: []string{"ops@corp.test"}})
	defer ch.(*asyncSender).Close()

	_ = ch.Send(context.Background(), Notification{ID: "n1", Severity: Info})
	waitFor(t, "the message", func() bool { return len(s.snapshot()) == 1 })
	if s.snapshot()[0].Subject != "Notification" {
		t.Errorf("subject = %q, want the fallback", s.snapshot()[0].Subject)
	}
}
