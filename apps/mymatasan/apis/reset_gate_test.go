package apis

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResetGate(t *testing.T) {
	resetting := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	h := NewResetGate(func() bool { return resetting })(next)

	// Not resetting: passes through.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/auth/session", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("idle: got %d, want 200", rec.Code)
	}

	resetting = true

	// Resetting: DB-backed request is shed with 503 instead of a raw 500.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/auth/session", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("resetting login probe: got %d, want 503", rec.Code)
	}

	// Resetting: the reset progress endpoint must stay reachable for the overlay to poll.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/system/reset/progress", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("resetting progress poll: got %d, want 200 (must stay reachable)", rec.Code)
	}
}
