package mailer

import (
	"encoding/base64"
	"fmt"
	"mime"
	"sort"
	"strings"
	"time"
)

// Attachment is one file carried with a message (e.g. a detection snapshot).
type Attachment struct {
	// Filename is the name the recipient sees.
	Filename string
	// ContentType is the MIME type (e.g. "image/jpeg"). Defaults to
	// application/octet-stream when empty.
	ContentType string
	// Data is the raw file content.
	Data []byte
}

// Message is one outbound mail. It exists alongside the older single-recipient
// Send so a caller that needs several recipients, custom headers, or an
// attachment does not have to send the same alert several times over several
// SMTP conversations.
type Message struct {
	// To is the recipient list. Every address becomes an envelope RCPT and is
	// listed in the To header.
	To []string
	// Subject is the message subject.
	Subject string
	// Body is the plain-text body.
	Body string
	// Headers are extra RFC 5322 headers (e.g. X-Kopiv2-Severity) so a recipient
	// can build mail rules without parsing the body. Reserved headers (From, To,
	// Subject, Date, MIME-Version, Content-Type) are ignored here — they are
	// assembled from the fields above.
	Headers map[string]string
	// Attachments are optional files sent as a multipart/mixed message.
	Attachments []Attachment
}

// reservedHeaders are assembled from Message's own fields and may not be
// overridden through Headers — otherwise a templated value could replace the
// Content-Type of a multipart message and detach the body from its parts.
var reservedHeaders = map[string]bool{
	"from": true, "to": true, "subject": true, "date": true,
	"mime-version": true, "content-type": true,
	"content-transfer-encoding": true,
}

// build assembles the RFC 5322 message. Header values are stripped of CR/LF so
// no templated value can smuggle a new header line, and non-ASCII subjects are
// RFC 2047 encoded rather than emitted raw (a raw UTF-8 subject is mangled or
// rejected by strict relays).
func (m Message) build(from string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", stripHeader(from))
	fmt.Fprintf(&b, "To: %s\r\n", stripHeader(strings.Join(m.To, ", ")))
	fmt.Fprintf(&b, "Subject: %s\r\n", encodeHeaderWord(stripHeader(m.Subject)))
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().UTC().Format(time.RFC1123Z))
	for _, k := range sortedHeaderKeys(m.Headers) {
		if reservedHeaders[strings.ToLower(strings.TrimSpace(k))] {
			continue
		}
		fmt.Fprintf(&b, "%s: %s\r\n", stripHeader(k), encodeHeaderWord(stripHeader(m.Headers[k])))
	}
	b.WriteString("MIME-Version: 1.0\r\n")

	if len(m.Attachments) == 0 {
		b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
		b.WriteString("\r\n")
		b.WriteString(crlf(m.Body))
		return b.String()
	}

	// A fixed boundary is safe because every part is base64 or plain text that
	// cannot contain it: it embeds a token no encoder emits.
	const boundary = "kopiv2-mailer-boundary-8f3a1c"
	fmt.Fprintf(&b, "Content-Type: multipart/mixed; boundary=%q\r\n", boundary)
	b.WriteString("\r\n")
	fmt.Fprintf(&b, "--%s\r\n", boundary)
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n\r\n")
	b.WriteString(crlf(m.Body))
	b.WriteString("\r\n")
	for _, a := range m.Attachments {
		ct := strings.TrimSpace(a.ContentType)
		if ct == "" {
			ct = "application/octet-stream"
		}
		name := stripHeader(a.Filename)
		if name == "" {
			name = "attachment"
		}
		fmt.Fprintf(&b, "--%s\r\n", boundary)
		fmt.Fprintf(&b, "Content-Type: %s\r\n", stripHeader(ct))
		b.WriteString("Content-Transfer-Encoding: base64\r\n")
		fmt.Fprintf(&b, "Content-Disposition: attachment; filename=%q\r\n\r\n", name)
		b.WriteString(wrapBase64(a.Data))
		b.WriteString("\r\n")
	}
	fmt.Fprintf(&b, "--%s--\r\n", boundary)
	return b.String()
}

// crlf normalises a body to CRLF line endings.
func crlf(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\n", "\r\n")
}

// encodeHeaderWord RFC 2047 encodes a header value when it is not pure ASCII.
// Alert subjects carry camera and rule names, which in this suite are routinely
// Malay, Chinese or Arabic.
func encodeHeaderWord(v string) string {
	for i := 0; i < len(v); i++ {
		if v[i] > 127 {
			return mime.QEncoding.Encode("utf-8", v)
		}
	}
	return v
}

// wrapBase64 encodes data and folds it to 76-character lines, which RFC 2045
// requires and some relays enforce by rejecting the message.
func wrapBase64(data []byte) string {
	enc := base64.StdEncoding.EncodeToString(data)
	var b strings.Builder
	for len(enc) > 76 {
		b.WriteString(enc[:76])
		b.WriteString("\r\n")
		enc = enc[76:]
	}
	b.WriteString(enc)
	return b.String()
}

// sortedHeaderKeys orders extra headers so a built message is byte-identical for
// the same input — Go map iteration is randomised, which would otherwise make
// every assertion on a rendered message flaky.
func sortedHeaderKeys(h map[string]string) []string {
	if len(h) == 0 {
		return nil
	}
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
