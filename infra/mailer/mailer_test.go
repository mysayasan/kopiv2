package mailer

import (
	"encoding/base64"
	"strings"
	"testing"
)

// TestBuildMessageStripsHeaderInjection ensures templated values cannot smuggle
// extra headers or a body via embedded CR/LF — the classic email-header-injection
// class. Subject/From/To must each stay on one header line.
func TestBuildMessageStripsHeaderInjection(t *testing.T) {
	msg := Message{
		To:      []string{"victim@corp.test\r\nBcc: attacker@evil.test"},
		Subject: "Reset\r\nX-Injected: yes",
		Body:    "line one\nline two",
		// A templated header value must not open a new header line either — the
		// notification channel puts notification-derived text into these.
		Headers: map[string]string{"X-Kopiv2-Source": "settings\r\nBcc: attacker@evil.test"},
	}.build("noreply@myidsan.test")

	// The injection is defeated by stripping CR/LF from header values, so the crafted
	// tokens survive only as inert inline text on the legitimate header line — never
	// as a NEW header line. Assert no line STARTS with an injected header name.
	for _, line := range strings.Split(msg, "\r\n") {
		if strings.HasPrefix(line, "Bcc:") || strings.HasPrefix(line, "X-Injected:") {
			t.Fatalf("header injection created a new header line: %q\nfull:\n%s", line, msg)
		}
	}
	// Body CRLF normalisation: a lone \n becomes \r\n.
	if !strings.Contains(msg, "line one\r\nline two") {
		t.Errorf("body not CRLF-normalised:\n%q", msg)
	}
}

// TestBuildMessageRejectsReservedHeaderOverride ensures a caller-supplied header
// cannot replace one the builder assembles. Overriding Content-Type on a
// multipart message would detach the body from its parts and hide the alert text.
func TestBuildMessageRejectsReservedHeaderOverride(t *testing.T) {
	msg := Message{
		To:          []string{"ops@corp.test"},
		Subject:     "Alert",
		Body:        "body text",
		Headers:     map[string]string{"Content-Type": "text/plain", "To": "attacker@evil.test"},
		Attachments: []Attachment{{Filename: "snap.jpg", ContentType: "image/jpeg", Data: []byte{0xFF, 0xD8}}},
	}.build("nvr@corp.test")

	if strings.Count(msg, "\r\nContent-Type: multipart/mixed") != 1 {
		t.Errorf("multipart Content-Type not present exactly once:\n%s", msg)
	}
	if strings.Contains(msg, "attacker@evil.test") {
		t.Errorf("reserved To header was overridable:\n%s", msg)
	}
}

// TestBuildMessageEncodesNonAsciiSubject asserts a non-ASCII subject is RFC 2047
// encoded. Camera and rule names in this suite are routinely Malay, Chinese or
// Arabic; a raw UTF-8 subject is mangled or rejected by a strict relay.
func TestBuildMessageEncodesNonAsciiSubject(t *testing.T) {
	msg := Message{To: []string{"ops@corp.test"}, Subject: "Amaran: Pintu 门 مفتوح", Body: "x"}.
		build("nvr@corp.test")
	subject := headerLine(t, msg, "Subject")
	if strings.Contains(subject, "门") {
		t.Errorf("non-ASCII subject emitted raw: %q", subject)
	}
	if !strings.Contains(subject, "=?utf-8?") {
		t.Errorf("subject not RFC 2047 encoded: %q", subject)
	}
	// ASCII subjects must stay readable rather than being needlessly encoded.
	plain := headerLine(t, Message{To: []string{"a@b.test"}, Subject: "Camera offline", Body: "x"}.build("n@c.test"), "Subject")
	if plain != "Subject: Camera offline" {
		t.Errorf("ASCII subject was altered: %q", plain)
	}
}

// TestBuildMessageAttachmentIsDecodable asserts the attachment survives the
// encoding intact. A snapshot that arrives corrupt is worse than none: the
// operator believes they have looked at the evidence.
func TestBuildMessageAttachmentIsDecodable(t *testing.T) {
	// Long enough to exercise the 76-column base64 folding.
	raw := make([]byte, 500)
	for i := range raw {
		raw[i] = byte(i % 251)
	}
	msg := Message{
		To:          []string{"ops@corp.test"},
		Subject:     "Alert",
		Body:        "person detected",
		Attachments: []Attachment{{Filename: "alert-7.jpg", ContentType: "image/jpeg", Data: raw}},
	}.build("nvr@corp.test")

	_, after, ok := strings.Cut(msg, "Content-Disposition: attachment; filename=\"alert-7.jpg\"\r\n\r\n")
	if !ok {
		t.Fatalf("attachment part not found:\n%s", msg)
	}
	encoded, _, ok := strings.Cut(after, "\r\n--")
	if !ok {
		t.Fatalf("attachment part not terminated by a boundary:\n%s", msg)
	}
	for _, line := range strings.Split(encoded, "\r\n") {
		if len(line) > 76 {
			t.Errorf("base64 line exceeds 76 chars (%d)", len(line))
		}
	}
	got, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(encoded, "\r\n", ""))
	if err != nil {
		t.Fatalf("attachment did not decode: %v", err)
	}
	if string(got) != string(raw) {
		t.Errorf("attachment corrupted in transit: %d bytes in, %d out", len(raw), len(got))
	}
	// The body must still be present as its own part.
	if !strings.Contains(msg, "person detected") {
		t.Errorf("body lost when an attachment was added:\n%s", msg)
	}
}

func TestMailerEnabledGating(t *testing.T) {
	if (&Mailer{cfg: Config{Enabled: false, Host: "h"}}).Enabled() {
		t.Error("disabled mailer reported enabled")
	}
	if (&Mailer{cfg: Config{Enabled: true, Host: ""}}).Enabled() {
		t.Error("hostless mailer reported enabled")
	}
	if !(&Mailer{cfg: Config{Enabled: true, Host: "relay"}}).Enabled() {
		t.Error("configured mailer reported disabled")
	}
	var nilMailer *Mailer
	if nilMailer.Enabled() {
		t.Error("nil mailer reported enabled")
	}
}

func TestNormalizeRecipients(t *testing.T) {
	got := normalizeRecipients([]string{" ops@corp.test ", "", "a@b.test, c@d.test", "OPS@corp.test", "  "})
	want := []string{"ops@corp.test", "a@b.test", "c@d.test"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("normalizeRecipients = %v, want %v", got, want)
	}
}

// headerLine returns the first header line with the given name.
func headerLine(t *testing.T, msg, name string) string {
	t.Helper()
	for _, line := range strings.Split(msg, "\r\n") {
		if line == "" {
			break // end of headers
		}
		if strings.HasPrefix(line, name+":") {
			return line
		}
	}
	t.Fatalf("header %q not found in:\n%s", name, msg)
	return ""
}
