// Package mailer is a minimal SMTP sender for the OPTIONAL internal-relay features
// (today: myidsan's self-service password-reset link). It is pure standard library
// (net/smtp) and only ever connects to the operator-configured relay — an air-gapped
// install simply leaves it disabled, and nothing reaches for a network. There is no
// third-party dependency and no external egress.
package mailer

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"strconv"
	"strings"
)

// Config is the SMTP relay connection, mirrored from the app config `smtp` block.
type Config struct {
	Enabled     bool
	Host        string
	Port        int
	From        string
	Username    string
	Password    string
	UseStartTls bool
}

// Mailer sends plain-text mail through the configured relay. A nil or disabled
// Mailer reports Enabled()==false so callers can skip the email path cleanly.
type Mailer struct {
	cfg Config
}

func New(cfg Config) *Mailer { return &Mailer{cfg: cfg} }

// Enabled is true only when mail is switched on AND a relay host is configured.
func (m *Mailer) Enabled() bool {
	return m != nil && m.cfg.Enabled && strings.TrimSpace(m.cfg.Host) != ""
}

// From returns the configured envelope/sender address (falls back to username).
func (m *Mailer) From() string {
	if s := strings.TrimSpace(m.cfg.From); s != "" {
		return s
	}
	return strings.TrimSpace(m.cfg.Username)
}

// Send delivers a plain-text message to a single recipient. It is a thin wrapper
// over SendMessage, kept because myidsan's password-reset path has no need of
// recipient lists or attachments.
func (m *Mailer) Send(to, subject, body string) error {
	return m.SendMessage(Message{To: []string{to}, Subject: subject, Body: body})
}

// SendMessage delivers one message to every recipient in a single SMTP
// conversation. It dials the relay, upgrades to TLS via STARTTLS when
// configured, authenticates when a username is set, and refuses to send
// credentials over an un-upgraded connection.
//
// PARTIAL DELIVERY IS A SUCCESS. When the relay rejects SOME recipients (a typo,
// a mailbox that has since been closed) the message is still delivered to the
// rest and the rejections are returned as a RecipientError alongside a nil-free
// send. Failing the whole send on one bad address would mean one stale entry in
// a distribution list silences an alert for everybody else on it — and because
// the notification channel retries transient failures, it would do so on every
// single alert, forever. Only a message that reached NOBODY is an error.
func (m *Mailer) SendMessage(msg Message) error {
	if !m.Enabled() {
		return errors.New("mailer is not configured")
	}
	rcpts := normalizeRecipients(msg.To)
	if len(rcpts) == 0 {
		return errors.New("empty recipient")
	}
	msg.To = rcpts

	from := m.From()
	if from == "" {
		return errors.New("smtp: no From address configured")
	}

	host := strings.TrimSpace(m.cfg.Host)
	port := m.cfg.Port
	if port == 0 {
		port = 587
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))

	c, err := smtp.Dial(addr)
	if err != nil {
		return fmt.Errorf("smtp dial %s: %w", addr, err)
	}
	defer c.Close()

	if err := c.Hello(smtpHelloName(from)); err != nil {
		return err
	}

	secured := false
	if m.cfg.UseStartTls {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return errors.New("smtp: STARTTLS requested but not offered by the relay")
		}
		if err := c.StartTLS(&tls.Config{ServerName: host}); err != nil {
			return fmt.Errorf("smtp starttls: %w", err)
		}
		secured = true
	}

	if strings.TrimSpace(m.cfg.Username) != "" {
		if !secured {
			// Never send a password over a cleartext link.
			return errors.New("smtp: authentication configured but connection is not secured (enable useStartTls)")
		}
		auth := smtp.PlainAuth("", m.cfg.Username, m.cfg.Password, host)
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}

	if err := c.Mail(from); err != nil {
		return err
	}
	var accepted []string
	var rejected []RejectedRecipient
	for _, to := range rcpts {
		if err := c.Rcpt(to); err != nil {
			rejected = append(rejected, RejectedRecipient{Address: to, Err: err})
			continue
		}
		accepted = append(accepted, to)
	}
	if len(accepted) == 0 {
		return &RecipientError{Rejected: rejected, AllRejected: true}
	}
	// The To header lists only the addresses the relay accepted, so the message a
	// recipient reads does not claim it went somewhere it did not.
	msg.To = accepted

	wc, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := wc.Write([]byte(msg.build(from))); err != nil {
		return err
	}
	if err := wc.Close(); err != nil {
		return err
	}
	if err := c.Quit(); err != nil {
		return err
	}
	if len(rejected) > 0 {
		return &RecipientError{Rejected: rejected}
	}
	return nil
}

// normalizeRecipients trims, drops blanks, and de-duplicates addresses
// case-insensitively on the domain while preserving order. A list pasted from a
// spreadsheet routinely contains the same address twice; sending the same alert
// to the same mailbox twice reads as a bug in the alerting, not in the list.
func normalizeRecipients(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		for _, part := range strings.Split(raw, ",") {
			addr := stripHeader(part)
			if addr == "" {
				continue
			}
			key := strings.ToLower(addr)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, addr)
		}
	}
	return out
}

func stripHeader(v string) string {
	return strings.NewReplacer("\r", "", "\n", "").Replace(strings.TrimSpace(v))
}

// smtpHelloName derives an EHLO name from the sender domain, defaulting to
// "localhost" when the From address has no usable domain.
func smtpHelloName(from string) string {
	if at := strings.LastIndex(from, "@"); at >= 0 && at+1 < len(from) {
		if d := strings.TrimSpace(from[at+1:]); d != "" {
			return d
		}
	}
	return "localhost"
}
