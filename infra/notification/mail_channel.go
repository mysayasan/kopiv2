package notification

import (
	"fmt"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/infra/mailer"
)

// MailOptions configures the outbound email delivery channel. It is the channel
// procurement asks for: unlike a webhook or a bot, a mailbox is something every
// organisation already operates, audits and retains.
//
// The RELAY (host, credentials, sender) is deliberately NOT part of this struct
// per destination — it arrives as one Relay for the whole install. An SMTP relay
// is infrastructure, not a per-recipient choice: copying its password onto every
// destination row would multiply the secret to rotate, and would let a
// notification screen quietly open egress on an install whose config says mail
// is off.
type MailOptions struct {
	// Relay is the SMTP connection. A disabled or hostless relay yields a no-op
	// channel.
	Relay mailer.Config
	// To is the recipient list. Empty disables the channel.
	To []string
	// SubjectPrefix is prepended to every subject (e.g. "[Site A]"), so a
	// recipient receiving mail from several installs can tell them apart and
	// filter on it.
	SubjectPrefix string
	// IncludeSnapshot attaches the notification's image when it carries one.
	IncludeSnapshot bool
	// MinSeverity skips notifications below this severity. Empty means no floor.
	MinSeverity Severity
	// QueueSize bounds the internal buffer. Defaults to 256.
	QueueSize int
	// Sender overrides the mail transport (used by tests).
	Sender MailSender
	// Logger receives delivery warnings.
	Logger Logger
}

// MailSender is the transport the channel delivers through. infra/mailer.Mailer
// satisfies it; tests substitute their own.
type MailSender interface {
	SendMessage(msg mailer.Message) error
}

// NewMailChannel returns a channel that emails notifications through the
// configured relay. When mail is disabled, the relay has no host, or no
// recipient is configured, the channel is a no-op — so it can always be
// registered and switched on later by configuration.
func NewMailChannel(opts MailOptions) Channel {
	sender := opts.Sender
	if sender == nil {
		m := mailer.New(opts.Relay)
		if !m.Enabled() {
			return noopChannel{name: "email"}
		}
		sender = m
	}
	if len(cleanRecipients(opts.To)) == 0 {
		return noopChannel{name: "email"}
	}
	s := &mailSender{
		sender:    sender,
		to:        cleanRecipients(opts.To),
		prefix:    strings.TrimSpace(opts.SubjectPrefix),
		snapshots: opts.IncludeSnapshot,
		logger:    opts.Logger,
	}
	return newAsyncSender("email", opts.MinSeverity, opts.QueueSize, s.send, opts.Logger)
}

type mailSender struct {
	sender    MailSender
	to        []string
	prefix    string
	snapshots bool
	logger    Logger
}

// send delivers one notification as mail.
//
// The error contract matters more than it looks: the async worker RETRIES
// whatever this returns unless it is marked permanent. A relay that is down is
// worth retrying; a recipient the relay refuses by name is not, and neither is a
// message that was in fact delivered to everyone else. Getting that wrong sends
// the same alert four times to the addresses that work.
func (s *mailSender) send(n Notification) error {
	msg := mailer.Message{
		To:      s.to,
		Subject: s.subject(n),
		Body:    mailBody(n),
		Headers: map[string]string{
			// Header-based classification lets a recipient build mailbox rules
			// without parsing the body — the thing an ops team asks for first.
			"X-Kopiv2-Severity": string(n.Severity.Normalize()),
			"X-Kopiv2-Category": n.Category,
			"X-Kopiv2-Source":   n.Source,
		},
	}
	if s.snapshots && n.Attachment != nil && len(n.Attachment.Data) > 0 {
		filename := strings.TrimSpace(n.Attachment.Filename)
		if filename == "" {
			filename = "snapshot.jpg"
		}
		msg.Attachments = []mailer.Attachment{{
			Filename:    filename,
			ContentType: n.Attachment.ContentType,
			Data:        n.Attachment.Data,
		}}
	}

	err := s.sender.SendMessage(msg)
	if err == nil {
		return nil
	}
	if re, ok := err.(*mailer.RecipientError); ok {
		if re.Delivered() {
			// Delivered to the rest: report the rejections and DO NOT retry.
			warn(s.logger, "notification.email", "delivered notification %s, but the relay rejected %s",
				n.ID, strings.Join(re.Addresses(), ", "))
			return nil
		}
		// Nobody got it, and the relay named the addresses — retrying the same
		// list cannot change that answer.
		return permanent(re)
	}
	return err
}

// subject renders the mail subject: an optional install prefix, the severity for
// anything above info, and the notification title.
func (s *mailSender) subject(n Notification) string {
	var b strings.Builder
	if s.prefix != "" {
		b.WriteString(s.prefix)
		b.WriteString(" ")
	}
	switch n.Severity.Normalize() {
	case Critical:
		b.WriteString("[CRITICAL] ")
	case Warning:
		b.WriteString("[WARNING] ")
	}
	title := strings.TrimSpace(n.Title)
	if title == "" {
		title = "Notification"
	}
	b.WriteString(title)
	return truncateRunes(b.String(), 200)
}

// mailBody renders the plain-text body: the message, then the context an
// operator needs to act on it without opening the app, then the deep link.
func mailBody(n Notification) string {
	var b strings.Builder
	if body := strings.TrimSpace(n.Body); body != "" {
		b.WriteString(body)
		b.WriteString("\n\n")
	}
	fmt.Fprintf(&b, "Severity: %s\n", n.Severity.Normalize())
	if n.Category != "" {
		fmt.Fprintf(&b, "Category: %s\n", n.Category)
	}
	if n.Source != "" {
		fmt.Fprintf(&b, "Source:   %s\n", n.Source)
	}
	if ts := n.CreatedAt; ts > 0 {
		// Always UTC and always labelled. An unlabelled local timestamp in an
		// email read on another continent is worse than no timestamp.
		fmt.Fprintf(&b, "Time:     %s\n", time.Unix(ts, 0).UTC().Format("2006-01-02 15:04:05 UTC"))
	}
	if n.Link != "" {
		fmt.Fprintf(&b, "\n%s\n", n.Link)
	}
	return b.String()
}

// cleanRecipients trims and drops blank addresses, splitting comma-separated
// entries so a pasted list works as typed.
func cleanRecipients(in []string) []string {
	out := make([]string, 0, len(in))
	for _, raw := range in {
		for _, part := range strings.Split(raw, ",") {
			if v := strings.TrimSpace(part); v != "" {
				out = append(out, v)
			}
		}
	}
	return out
}
