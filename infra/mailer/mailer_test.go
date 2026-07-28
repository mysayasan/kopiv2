package mailer

import "strings"

import "testing"

// TestBuildMessageStripsHeaderInjection ensures templated values cannot smuggle
// extra headers or a body via embedded CR/LF — the classic email-header-injection
// class. Subject/From/To must each stay on one header line.
func TestBuildMessageStripsHeaderInjection(t *testing.T) {
	msg := buildMessage(
		"noreply@myidsan.test",
		"victim@corp.test\r\nBcc: attacker@evil.test",
		"Reset\r\nX-Injected: yes",
		"line one\nline two",
	)
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
