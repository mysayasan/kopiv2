package apis

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

// SystemResetHandlers serves the factory-reset endpoints. Route registration stays with
// each app because they differ in how they gate an admin write, but the payloads are
// identical so one SPA overlay works everywhere.
type SystemResetHandlers struct {
	reset *sharedservices.SystemResetService
}

func NewSystemResetHandlers(reset *sharedservices.SystemResetService) *SystemResetHandlers {
	return &SystemResetHandlers{reset: reset}
}

// State tells the UI whether the reset is available and what the operator has to type.
// The phrase comes from the server so the instruction and the check can never drift
// apart -- a dialog that asks for one word while the server expects another would fail
// confusingly at the worst possible moment.
func (h *SystemResetHandlers) State(w http.ResponseWriter, r *http.Request) {
	controllers.SendResult(w, map[string]any{
		"allowed":       h.reset.Allowed(),
		"confirmPhrase": h.reset.ConfirmPhrase(),
	}, "succeed")
}

// Start begins a factory reset after checking the typed confirmation, and returns the
// initial progress. The wipe and restart proceed in the background; the client polls
// Progress.
func (h *SystemResetHandlers) Start(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Confirm string `json:"confirm"`
	}
	// A missing or unparseable body is treated as an empty confirmation rather than a
	// separate error, so the mismatch message is the one an operator sees either way.
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	if err := h.reset.Start(strings.TrimSpace(body.Confirm)); err != nil {
		if errors.Is(err, sharedservices.ErrConfirmMismatch) {
			controllers.SendError(w, controllers.ErrBadRequest, err.Error())
			return
		}
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, h.reset.Progress(), "succeed")
}

// Progress returns the in-memory status of a running or finished reset.
func (h *SystemResetHandlers) Progress(w http.ResponseWriter, r *http.Request) {
	controllers.SendResult(w, h.reset.Progress(), "succeed")
}

// NewResetGate short-circuits protected requests with a clean 503 while a reset runs,
// and serves the progress endpoint itself for the duration.
//
// Both halves exist because the reset closes the database pool before restarting. Without
// the 503, every DB-backed request -- including the session probe the SPA polls --
// returns a raw 500 for that window and the overlay looks like a crash. And without
// serving progress here, progress is unreachable too: it sits behind auth and permission
// middleware that need the very database the reset just dropped, so a live run showed it
// answering "access denied" from the moment the pool closed, leaving the operator blind
// at 60% through the most destructive part of the wipe.
//
// Serving progress from in front of the auth middleware means that, WHILE A RESET IS
// RUNNING, anyone who can reach the port can read the stage and percentage. That payload
// carries no secrets -- a stage name, a percentage, a status message -- and the reset is
// an operator-initiated action whose restart is externally visible anyway. Outside a
// reset the request falls straight through to the normal authenticated handler, so this
// does not open the endpoint up in general.
func NewResetGate(reset *sharedservices.SystemResetService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Read per request: the service is constructed after the middleware is wired.
			if reset == nil || !reset.InProgress() {
				next.ServeHTTP(w, r)
				return
			}
			if strings.HasSuffix(r.URL.Path, "/system/reset/progress") {
				controllers.SendResult(w, reset.Progress(), "succeed")
				return
			}
			if strings.Contains(r.URL.Path, "/system/reset") {
				next.ServeHTTP(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "5")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":  "failed",
				"message": "factory reset in progress",
				"code":    "reset_in_progress",
			})
		})
	}
}
