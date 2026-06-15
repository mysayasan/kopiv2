package middlewares

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Compile-time guard: the request-logging wrapper must remain an http.Flusher so
// streaming responses (the SSE notification feed) keep working through it.
var _ http.Flusher = (*statusWriter)(nil)

// flushRecorder is an httptest.ResponseRecorder that records whether Flush ran.
type flushRecorder struct {
	*httptest.ResponseRecorder
	flushed bool
}

func (f *flushRecorder) Flush() { f.flushed = true }

func TestStatusWriterForwardsFlush(t *testing.T) {
	rec := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	sw := &statusWriter{ResponseWriter: rec, status: http.StatusOK}

	flusher, ok := interface{}(sw).(http.Flusher)
	if !ok {
		t.Fatal("statusWriter must implement http.Flusher for SSE streaming")
	}
	flusher.Flush()
	if !rec.flushed {
		t.Fatal("Flush was not forwarded to the underlying ResponseWriter")
	}
}

func TestStatusWriterFlushSafeWhenUnderlyingHasNoFlush(t *testing.T) {
	// A non-flushing underlying writer must not panic when Flush is called.
	sw := &statusWriter{ResponseWriter: nopResponseWriter{}, status: http.StatusOK}
	sw.Flush()
}

// nopResponseWriter is a minimal ResponseWriter that does not implement Flusher.
type nopResponseWriter struct{}

func (nopResponseWriter) Header() http.Header         { return http.Header{} }
func (nopResponseWriter) Write(b []byte) (int, error) { return len(b), nil }
func (nopResponseWriter) WriteHeader(int)             {}
