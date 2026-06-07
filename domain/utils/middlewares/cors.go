package middlewares

import (
	"net/http"
	"strings"
)

// CorsMidware struct
type CorsMidware struct {
	allowAll       bool
	allowedOrigins map[string]struct{}
}

// Init
func NewCors(allowOrigins string) *CorsMidware {
	m := &CorsMidware{allowedOrigins: map[string]struct{}{}}
	for _, raw := range strings.Split(allowOrigins, ",") {
		origin := strings.TrimSpace(raw)
		if origin == "" {
			continue
		}
		if origin == "*" {
			m.allowAll = true
			continue
		}
		m.allowedOrigins[origin] = struct{}{}
	}
	return m
}

// Cors
func (m *CorsMidware) CorsHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.writeHeaders(w, r)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (m *CorsMidware) writeHeaders(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Accept,"+CSRFHeaderName)
	w.Header().Set("Access-Control-Expose-Headers", CSRFHeaderName)

	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return
	}

	if m.isOriginAllowed(origin) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Vary", "Origin")
		return
	}

	if m.allowAll {
		w.Header().Set("Access-Control-Allow-Origin", "*")
	}
}

func (m *CorsMidware) isOriginAllowed(origin string) bool {
	_, ok := m.allowedOrigins[origin]
	return ok
}
