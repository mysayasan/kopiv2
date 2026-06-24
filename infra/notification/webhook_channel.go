package notification

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// WebhookOptions configures the outbound webhook channel.
type WebhookOptions struct {
	// URL is the endpoint notifications are POSTed to as JSON. Empty disables
	// the channel (a no-op channel is returned).
	URL string
	// Method defaults to POST.
	Method string
	// Headers are added to every request (e.g. Authorization).
	Headers map[string]string
	// MinSeverity skips notifications below this severity. Empty means no floor.
	MinSeverity Severity
	// Timeout bounds each HTTP request. Defaults to 8s.
	Timeout time.Duration
	// QueueSize bounds the internal buffer. Defaults to 256.
	QueueSize int
	// Client overrides the HTTP client (mainly for tests).
	Client *http.Client
	// Logger receives drop/delivery warnings.
	Logger Logger
}

// NewWebhookChannel returns a webhook channel. When opts.URL is empty the
// channel is a no-op (so it can always be registered and enabled later by
// configuration).
func NewWebhookChannel(opts WebhookOptions) Channel {
	if strings.TrimSpace(opts.URL) == "" {
		return noopChannel{name: "webhook"}
	}
	method := opts.Method
	if method == "" {
		method = http.MethodPost
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	client := opts.Client
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	w := &webhookSender{
		url:     opts.URL,
		method:  method,
		headers: opts.Headers,
		client:  client,
		logger:  opts.Logger,
	}
	return newAsyncSender("webhook", opts.MinSeverity, opts.QueueSize, w.post, opts.Logger)
}

type webhookSender struct {
	url     string
	method  string
	headers map[string]string
	client  *http.Client
	logger  Logger
}

// post delivers one notification. It returns a (retryable) error on a transient
// failure so the async worker can retry; marshal/build failures are wrapped as
// permanent since retrying cannot fix them. Final logging is done by the worker.
func (w *webhookSender) post(n Notification) error {
	// Shared canonical JSON (Notification + base64 snapshot when attached) — the
	// same shape the MQTT channel publishes. See notificationJSON.
	body, err := notificationJSON(n)
	if err != nil {
		return permanent(fmt.Errorf("marshal failed: %w", err))
	}
	ctx, cancel := context.WithTimeout(context.Background(), w.client.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, w.method, w.url, bytes.NewReader(body))
	if err != nil {
		return permanent(fmt.Errorf("build request failed: %w", err))
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range w.headers {
		req.Header.Set(k, v)
	}
	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("post failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("post returned %s", resp.Status)
	}
	return nil
}

// noopChannel is a disabled channel that silently accepts notifications.
type noopChannel struct{ name string }

func (c noopChannel) Name() string                             { return c.name }
func (c noopChannel) Send(context.Context, Notification) error { return nil }

func warn(logger Logger, source, format string, args ...any) {
	if logger != nil {
		logger.Warnf(source, format, args...)
	}
}
