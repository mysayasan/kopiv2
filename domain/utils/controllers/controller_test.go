package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type timedRecorder struct {
	*httptest.ResponseRecorder
	durationMs int64
}

func (r timedRecorder) RequestDurationMs() int64 {
	return r.durationMs
}

func TestSendResultIncludesDurationMs(t *testing.T) {
	w := timedRecorder{ResponseRecorder: httptest.NewRecorder(), durationMs: 42}

	if err := SendResult(w, map[string]bool{"ok": true}, "succeed"); err != nil {
		t.Fatalf("SendResult failed: %v", err)
	}

	var resp struct {
		Message    string          `json:"message"`
		DurationMs int64           `json:"durationMs"`
		Result     json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.DurationMs != 42 {
		t.Fatalf("durationMs got %d want 42", resp.DurationMs)
	}
}

func TestSendPagingResultIncludesDurationMs(t *testing.T) {
	w := timedRecorder{ResponseRecorder: httptest.NewRecorder(), durationMs: 7}

	if err := SendPagingResult(w, []string{"a"}, 10, 0, 1, "succeed"); err != nil {
		t.Fatalf("SendPagingResult failed: %v", err)
	}

	var resp struct {
		DurationMs int64 `json:"durationMs"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.DurationMs != 7 {
		t.Fatalf("durationMs got %d want 7", resp.DurationMs)
	}
}

func TestSendErrorIncludesDurationMs(t *testing.T) {
	w := timedRecorder{ResponseRecorder: httptest.NewRecorder(), durationMs: 15}

	if err := SendError(w, ErrBadRequest, "bad request"); err != nil {
		t.Fatalf("SendError failed: %v", err)
	}

	var resp struct {
		StatsCode  int   `json:"statsCode"`
		DurationMs int64 `json:"durationMs"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.StatsCode != http.StatusBadRequest {
		t.Fatalf("status got %d want %d", resp.StatsCode, http.StatusBadRequest)
	}
	if resp.DurationMs != 15 {
		t.Fatalf("durationMs got %d want 15", resp.DurationMs)
	}
}
