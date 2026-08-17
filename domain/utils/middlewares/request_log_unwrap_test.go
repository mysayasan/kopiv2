package middlewares

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// deadlineWriter stands in for the real connection: it records whether the write
// deadline was reached through the wrapper.
type deadlineWriter struct {
	http.ResponseWriter
	deadlineSet bool
}

func (d *deadlineWriter) SetWriteDeadline(time.Time) error {
	d.deadlineSet = true
	return nil
}

// A streaming handler must be able to clear its write deadline through this wrapper.
//
// The server applies WriteTimeout (30s by default) as an ABSOLUTE deadline from the start
// of a request, and it does not reset as more is written — so a Server-Sent Events stream
// is cut 30 seconds after it opens unless the handler opts out. http.ResponseController
// can only do that if every wrapper in the chain exposes what it wraps. When it cannot,
// the failure is close to invisible: a browser's EventSource reconnects silently and the
// bell keeps working, while every notification landing in a reconnect gap is lost.
func TestStatusWriterAllowsClearingTheWriteDeadline(t *testing.T) {
	inner := &deadlineWriter{ResponseWriter: httptest.NewRecorder()}
	wrapped := &statusWriter{ResponseWriter: inner}

	if err := http.NewResponseController(wrapped).SetWriteDeadline(time.Time{}); err != nil {
		t.Fatalf("SetWriteDeadline through the wrapper: %v", err)
	}
	if !inner.deadlineSet {
		t.Fatal("the deadline never reached the underlying writer — the wrapper is hiding it")
	}
}
