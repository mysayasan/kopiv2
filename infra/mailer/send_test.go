package mailer

import (
	"errors"
	"strings"
	"testing"
)

// TestSendMessageDeliversToEveryRecipient asserts one SMTP conversation carries
// every recipient, rather than one conversation per address.
func TestSendMessageDeliversToEveryRecipient(t *testing.T) {
	relay := newFakeRelay(t)
	m := New(relay.relayConfig("nvr@corp.test"))

	err := m.SendMessage(Message{
		To:      []string{"a@corp.test", "b@corp.test", "c@corp.test"},
		Subject: "Camera offline",
		Body:    "Lobby camera stopped responding.",
	})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	got := relay.received()
	if len(got) != 1 {
		t.Fatalf("expected 1 message, got %d", len(got))
	}
	if strings.Join(got[0].To, ",") != "a@corp.test,b@corp.test,c@corp.test" {
		t.Errorf("envelope recipients = %v", got[0].To)
	}
	if got[0].From != "nvr@corp.test" {
		t.Errorf("envelope sender = %q", got[0].From)
	}
	if !strings.Contains(got[0].Data, "Lobby camera stopped responding.") {
		t.Errorf("body missing from delivered message:\n%s", got[0].Data)
	}
}

// TestSendMessagePartialRejectionStillDelivers is the load-bearing assertion of
// this file. One stale address in a distribution list must not silence the alert
// for everyone else on it — and the caller must be able to tell that this DID
// deliver, so it does not retry and mail the working addresses again.
func TestSendMessagePartialRejectionStillDelivers(t *testing.T) {
	relay := newFakeRelay(t, "gone@corp.test")
	m := New(relay.relayConfig("nvr@corp.test"))

	err := m.SendMessage(Message{
		To:      []string{"gone@corp.test", "ops@corp.test"},
		Subject: "Intruder",
		Body:    "Person in restricted zone.",
	})

	var re *RecipientError
	if !errors.As(err, &re) {
		t.Fatalf("expected a *RecipientError reporting the rejection, got %v", err)
	}
	if !re.Delivered() {
		t.Fatalf("a send that reached ops@corp.test reported itself undelivered: %v", re)
	}
	if strings.Join(re.Addresses(), ",") != "gone@corp.test" {
		t.Errorf("rejected addresses = %v, want [gone@corp.test]", re.Addresses())
	}

	got := relay.received()
	if len(got) != 1 {
		t.Fatalf("the surviving recipient got %d messages, want 1", len(got))
	}
	if strings.Join(got[0].To, ",") != "ops@corp.test" {
		t.Errorf("envelope recipients = %v, want [ops@corp.test]", got[0].To)
	}
	// The To header must not claim delivery to an address the relay refused.
	if strings.Contains(headerLine(t, got[0].Data, "To"), "gone@corp.test") {
		t.Errorf("To header names a rejected recipient: %q", headerLine(t, got[0].Data, "To"))
	}
}

// TestSendMessageAllRejectedIsAFailure asserts the other half of the contract:
// when nobody received the message, it must NOT be reported as delivered.
func TestSendMessageAllRejectedIsAFailure(t *testing.T) {
	relay := newFakeRelay(t, "gone@corp.test", "also-gone@corp.test")
	m := New(relay.relayConfig("nvr@corp.test"))

	err := m.SendMessage(Message{
		To:      []string{"gone@corp.test", "also-gone@corp.test"},
		Subject: "Intruder",
		Body:    "Person in restricted zone.",
	})
	var re *RecipientError
	if !errors.As(err, &re) {
		t.Fatalf("expected *RecipientError, got %v", err)
	}
	if re.Delivered() {
		t.Error("a message nobody received reported itself delivered")
	}
	if len(relay.received()) != 0 {
		t.Errorf("relay accepted DATA with no recipients: %+v", relay.received())
	}
}

// TestSendMessageRefusesCleartextCredentials asserts the mailer will not hand a
// password to a connection it has not upgraded to TLS.
//
// The relay here OFFERS AUTH and would accept the credential, which is the case
// that actually matters: an operator who fills in a username and forgets to tick
// STARTTLS gets a relay that works, and a password crossing the network in the
// clear on every alert. The assertion is therefore that the password never
// reached the wire — not merely that the call returned an error.
func TestSendMessageRefusesCleartextCredentials(t *testing.T) {
	relay := newFakeRelay(t)
	relay.advertiseAuth = true
	cfg := relay.relayConfig("nvr@corp.test")
	cfg.Username = "nvr"
	cfg.Password = "s3cret"
	cfg.UseStartTls = false // the misconfiguration under test

	err := New(cfg).SendMessage(Message{To: []string{"ops@corp.test"}, Subject: "x", Body: "y"})
	if err == nil {
		t.Fatal("authenticated over a cleartext connection instead of refusing")
	}
	if !strings.Contains(err.Error(), "not secured") {
		t.Errorf("error does not name the cause: %v", err)
	}
	if got := relay.sawAuth(); len(got) != 0 {
		t.Errorf("credential reached the wire in cleartext: %v", got)
	}
	if len(relay.received()) != 0 {
		t.Error("message was delivered despite the refusal")
	}
}

// TestSendMessageRequiresOfferedStartTls asserts the separate guard: when the
// operator asked for STARTTLS and the relay does not offer it, the send fails
// rather than silently continuing in the clear.
func TestSendMessageRequiresOfferedStartTls(t *testing.T) {
	relay := newFakeRelay(t)
	cfg := relay.relayConfig("nvr@corp.test")
	cfg.UseStartTls = true

	err := New(cfg).SendMessage(Message{To: []string{"ops@corp.test"}, Subject: "x", Body: "y"})
	if err == nil {
		t.Fatal("sent in the clear although STARTTLS was requested")
	}
	if !strings.Contains(err.Error(), "STARTTLS") {
		t.Errorf("error does not name the cause: %v", err)
	}
	if len(relay.received()) != 0 {
		t.Error("message was delivered despite the refusal")
	}
}

// TestSendMessageRejectsEmptyRecipients keeps a misconfigured destination from
// opening a pointless SMTP conversation on every notification.
func TestSendMessageRejectsEmptyRecipients(t *testing.T) {
	relay := newFakeRelay(t)
	err := New(relay.relayConfig("nvr@corp.test")).
		SendMessage(Message{To: []string{"", "  "}, Subject: "x", Body: "y"})
	if err == nil {
		t.Fatal("expected an error for an empty recipient list")
	}
	if len(relay.received()) != 0 {
		t.Error("relay was contacted with no recipients")
	}
}

// TestSendMessageDotStuffing asserts a body line of "." does not terminate the
// DATA stream early — the message must arrive whole.
func TestSendMessageDotStuffing(t *testing.T) {
	relay := newFakeRelay(t)
	m := New(relay.relayConfig("nvr@corp.test"))
	if err := m.SendMessage(Message{
		To:      []string{"ops@corp.test"},
		Subject: "Alert",
		Body:    "first line\n.\nlast line",
	}); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	got := relay.received()
	if len(got) != 1 {
		t.Fatalf("expected 1 message, got %d", len(got))
	}
	if !strings.Contains(got[0].Data, "last line") {
		t.Errorf("message truncated at a lone dot:\n%s", got[0].Data)
	}
}
