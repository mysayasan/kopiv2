package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

// TelegramOptions configures the Telegram bot channel.
type TelegramOptions struct {
	// BotToken is the Telegram bot token (from @BotFather). Empty disables the
	// channel.
	BotToken string
	// ChatID is the destination chat/channel id. Empty disables the channel.
	ChatID string
	// MinSeverity skips notifications below this severity. Empty means no floor.
	MinSeverity Severity
	// APIBaseURL overrides the Telegram API base (mainly for tests). Defaults to
	// https://api.telegram.org.
	APIBaseURL string
	// Timeout bounds each HTTP request. Defaults to 8s.
	Timeout time.Duration
	// QueueSize bounds the internal buffer. Defaults to 256.
	QueueSize int
	// Client overrides the HTTP client (mainly for tests).
	Client *http.Client
	// Logger receives delivery warnings.
	Logger Logger
}

// NewTelegramChannel returns a channel that delivers notifications to a Telegram
// chat via the Bot API. When the token or chat id is empty the channel is a
// no-op.
func NewTelegramChannel(opts TelegramOptions) Channel {
	if strings.TrimSpace(opts.BotToken) == "" || strings.TrimSpace(opts.ChatID) == "" {
		return noopChannel{name: "telegram"}
	}
	base := strings.TrimRight(strings.TrimSpace(opts.APIBaseURL), "/")
	if base == "" {
		base = "https://api.telegram.org"
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	client := opts.Client
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	t := &telegramSender{
		apiBase: fmt.Sprintf("%s/bot%s", base, opts.BotToken),
		chatID:  opts.ChatID,
		client:  client,
		logger:  opts.Logger,
	}
	return newAsyncSender("telegram", opts.MinSeverity, opts.QueueSize, t.send)
}

type telegramSender struct {
	apiBase string
	chatID  string
	client  *http.Client
	logger  Logger
}

type telegramMessage struct {
	ChatID    string `json:"chat_id"`
	Text      string `json:"text"`
	ParseMode string `json:"parse_mode,omitempty"`
}

func (t *telegramSender) send(n Notification) {
	// When the notification carries an image, deliver it as a photo so the
	// snapshot shows inline in the chat; otherwise fall back to a text message.
	if n.Attachment != nil && len(n.Attachment.Data) > 0 {
		t.sendPhoto(n)
		return
	}
	t.sendMessage(n)
}

func (t *telegramSender) sendMessage(n Notification) {
	payload, err := json.Marshal(telegramMessage{
		ChatID:    t.chatID,
		Text:      telegramText(n),
		ParseMode: "HTML",
	})
	if err != nil {
		warn(t.logger, "notification.telegram", "marshal failed: %v", err)
		return
	}
	t.do("sendMessage", "application/json", bytes.NewReader(payload))
}

// sendPhoto uploads the attached image via multipart so it does not depend on the
// snapshot endpoint being publicly reachable. The notification text becomes the
// photo caption (Telegram caps captions at 1024 chars).
func (t *telegramSender) sendPhoto(n Notification) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("chat_id", t.chatID)
	_ = mw.WriteField("caption", truncateRunes(telegramText(n), 1024))
	_ = mw.WriteField("parse_mode", "HTML")
	filename := n.Attachment.Filename
	if strings.TrimSpace(filename) == "" {
		filename = "snapshot.jpg"
	}
	part, err := mw.CreateFormFile("photo", filename)
	if err != nil {
		warn(t.logger, "notification.telegram", "create photo part failed: %v", err)
		return
	}
	if _, err := part.Write(n.Attachment.Data); err != nil {
		warn(t.logger, "notification.telegram", "write photo failed: %v", err)
		return
	}
	if err := mw.Close(); err != nil {
		warn(t.logger, "notification.telegram", "close multipart failed: %v", err)
		return
	}
	t.do("sendPhoto", mw.FormDataContentType(), &buf)
}

// do POSTs to a Telegram Bot API method and logs non-2xx responses.
func (t *telegramSender) do(method, contentType string, body io.Reader) {
	ctx, cancel := context.WithTimeout(context.Background(), t.client.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.apiBase+"/"+method, body)
	if err != nil {
		warn(t.logger, "notification.telegram", "build request failed: %v", err)
		return
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := t.client.Do(req)
	if err != nil {
		warn(t.logger, "notification.telegram", "%s failed: %v", method, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		warn(t.logger, "notification.telegram", "%s returned %s", method, resp.Status)
	}
}

// truncateRunes shortens s to at most n runes without splitting a multi-byte rune.
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// telegramText renders a notification as an HTML-formatted Telegram message.
func telegramText(n Notification) string {
	var b strings.Builder
	if emoji := severityEmoji(n.Severity); emoji != "" {
		b.WriteString(emoji)
		b.WriteString(" ")
	}
	b.WriteString("<b>")
	b.WriteString(htmlEscape(n.Title))
	b.WriteString("</b>")
	if n.Body != "" {
		b.WriteString("\n")
		b.WriteString(htmlEscape(n.Body))
	}
	return b.String()
}

func severityEmoji(s Severity) string {
	switch s {
	case Critical:
		return "\U0001F6A8" // rotating light
	case Warning:
		return "⚠️" // warning sign
	case Info:
		return "ℹ️" // information
	default:
		return ""
	}
}

// htmlEscape escapes the characters Telegram's HTML parse mode requires.
func htmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
