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
	"time"
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

// Send delivers a plain-text message to a single recipient. It dials the relay,
// upgrades to TLS via STARTTLS when configured, authenticates when a username is
// set, and refuses to send credentials over an un-upgraded connection.
func (m *Mailer) Send(to, subject, body string) error {
	if !m.Enabled() {
		return errors.New("mailer is not configured")
	}
	to = strings.TrimSpace(to)
	if to == "" {
		return errors.New("empty recipient")
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

	if err := c.Hello(smtpHelloName(m.From())); err != nil {
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

	from := m.From()
	if from == "" {
		return errors.New("smtp: no From address configured")
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	wc, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := wc.Write([]byte(buildMessage(from, to, subject, body))); err != nil {
		return err
	}
	if err := wc.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// buildMessage assembles a minimal RFC 5322 plain-text message. Subject and header
// values are sanitised of CR/LF to prevent header injection from any templated input.
func buildMessage(from, to, subject, body string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", stripHeader(from))
	fmt.Fprintf(&b, "To: %s\r\n", stripHeader(to))
	fmt.Fprintf(&b, "Subject: %s\r\n", stripHeader(subject))
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().UTC().Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("\r\n")
	// Normalise the body to CRLF line endings.
	b.WriteString(strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\n", "\r\n"))
	return b.String()
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
