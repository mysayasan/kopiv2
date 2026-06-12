package middlewares

import (
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// RequestLogMidware emits request IDs and latency metrics for each API call.
type RequestLogMidware struct {
	logger requestLogger
}

type requestLogger interface {
	Infof(source string, format string, args ...any)
}

func NewRequestLog(logger ...requestLogger) *RequestLogMidware {
	m := &RequestLogMidware{}
	if len(logger) > 0 {
		m.logger = logger[0]
	}
	return m
}

type statusWriter struct {
	http.ResponseWriter
	status int
	start  time.Time
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) RequestDurationMs() int64 {
	if w == nil || w.start.IsZero() {
		return 0
	}
	return time.Since(w.start).Milliseconds()
}

func (m *RequestLogMidware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = uuid.NewString()
		}

		w.Header().Set("X-Request-ID", requestID)
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK, start: start}

		next.ServeHTTP(sw, r)

		msg := "rid=%s method=%s path=%s status=%d dur_ms=%d remote=%s"
		args := []any{requestID, r.Method, r.URL.Path, sw.status, time.Since(start).Milliseconds(), r.RemoteAddr}
		if m.logger != nil {
			m.logger.Infof("request", msg, args...)
			return
		}
		log.Printf(msg, args...)
	})
}
