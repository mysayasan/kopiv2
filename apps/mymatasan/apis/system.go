package apis

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

// restarter gracefully relaunches the running process. Satisfied by apphost.Restarter.
type restarter interface {
	Restart(reason string)
}

// NewResetGate short-circuits protected API requests with a clean 503 while a factory
// reset is running. The reset closes the database pool up front, then keeps the process
// alive through the slow free-space scrub before restarting — so without this gate every
// DB-backed request (including the login/session probe) returns a raw 500 for the whole
// window. Returning 503 "reset in progress" instead lets the SPA keep its reset overlay
// up and reload once the server restarts. It runs BEFORE the auth middleware so a request
// never touches the closed DB. The reset's own progress/state endpoints stay reachable so
// the overlay can keep polling; /health is public (outside this subrouter) and unaffected.
// isResetting is read per request because the reset service is created after this
// middleware is wired.
func NewResetGate(isResetting func() bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isResetting != nil && isResetting() && !strings.Contains(r.URL.Path, "/system/reset") {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "5")
				w.WriteHeader(http.StatusServiceUnavailable)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"status":  "failed",
					"message": "factory reset in progress",
					"code":    "reset_in_progress",
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// escrowKeyStore is the subset of atrest.KeyStore the recovery endpoints need. It stays an
// interface so this package doesn't depend on infra/atrest and to keep the seam testable.
type escrowKeyStore interface {
	Enabled() bool
	Protector() string
	ExportEscrow(passphrase string) ([]byte, error)
	VerifyEscrow(passphrase string, data []byte) (bool, error)
}

type systemApi struct {
	reset     *services.SystemResetService
	restarter restarter
	update    *services.UpdateService
	keystore  escrowKeyStore
}

// NewSystemApi registers system-level operations under /system. The factory reset and
// restart POSTs are admin-gated (write) by the router's require-admin middleware; reset
// is additionally guarded by bootstrap.allowReset in the service. keystore may be nil when
// encryption-at-rest is disabled, in which case the recovery endpoints report unavailable.
func NewSystemApi(router *mux.Router, reset *services.SystemResetService, restart restarter, update *services.UpdateService, keystore escrowKeyStore) {
	h := &systemApi{reset: reset, restarter: restart, update: update, keystore: keystore}
	g := router.PathPrefix("/system").Subrouter()
	g.HandleFunc("/reset", h.startReset).Methods("POST")
	g.HandleFunc("/reset/state", h.resetState).Methods("GET")
	g.HandleFunc("/reset/progress", h.resetProgress).Methods("GET")
	g.HandleFunc("/restart", h.restart).Methods("POST")
	g.HandleFunc("/time", h.time).Methods("GET")
	g.HandleFunc("/update", h.updateStatus).Methods("GET")
	g.HandleFunc("/update/check", h.updateCheck).Methods("POST")
	g.HandleFunc("/update/apply", h.updateApply).Methods("POST")
	g.HandleFunc("/recovery/state", h.recoveryState).Methods("GET")
	g.HandleFunc("/recovery/export", h.recoveryExport).Methods("POST")
	g.HandleFunc("/recovery/verify", h.recoveryVerify).Methods("POST")
}

// updateStatus returns the cached self-update status (current/latest version, whether
// an update is available, whether self-update can run here, and any in-flight apply).
func (a *systemApi) updateStatus(w http.ResponseWriter, r *http.Request) {
	if a.update == nil {
		controllers.SendError(w, controllers.ErrInternalServerError, "updater unavailable")
		return
	}
	controllers.SendResult(w, a.update.Status(), "succeed")
}

// updateCheck forces an immediate GitHub release check.
func (a *systemApi) updateCheck(w http.ResponseWriter, r *http.Request) {
	if a.update == nil {
		controllers.SendError(w, controllers.ErrInternalServerError, "updater unavailable")
		return
	}
	controllers.SendResult(w, a.update.CheckNow(r.Context()), "succeed")
}

// updateApply starts the background download + verify + swap + restart. Admin-gated.
func (a *systemApi) updateApply(w http.ResponseWriter, r *http.Request) {
	if a.update == nil {
		controllers.SendError(w, controllers.ErrInternalServerError, "updater unavailable")
		return
	}
	if err := a.update.StartUpdate(r.Context()); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, a.update.Status(), "succeed")
}

// time reports the host's timezone and current clock so the setup wizard can let the
// user confirm timestamps will be correct (read-only; no OS mutation).
func (a *systemApi) time(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	zone, offset := now.Zone()
	controllers.SendResult(w, map[string]any{
		"timezone":   now.Location().String(),
		"abbrev":     zone,
		"offsetSec":  offset,
		"now":        now.Format("2006-01-02 15:04:05"),
		"unix":       now.Unix(),
	}, "succeed")
}

// restart relaunches the process so changes that are only read at startup (e.g. a new
// ffmpeg path, switched Python, freshly installed GPU deps) take effect. The HTTP
// response is sent first; the relaunch happens a moment later so the client can begin
// polling for the server to come back.
func (a *systemApi) restart(w http.ResponseWriter, r *http.Request) {
	if a.restarter == nil {
		controllers.SendError(w, controllers.ErrInternalServerError, "restart is not available")
		return
	}
	controllers.SendResult(w, map[string]any{"restarting": true}, "succeed")
	go func() {
		time.Sleep(500 * time.Millisecond)
		a.restarter.Restart("api restart request")
	}()
}

// resetState tells the UI whether the factory reset is available, so the button can
// be hidden when bootstrap.allowReset is false.
func (a *systemApi) resetState(w http.ResponseWriter, r *http.Request) {
	controllers.SendResult(w, map[string]any{"allowed": a.reset.Allowed()}, "succeed")
}

// startReset begins a factory reset in the background and returns the initial
// progress. The actual wipe + restart proceed asynchronously; the client polls
// /reset/progress.
func (a *systemApi) startReset(w http.ResponseWriter, r *http.Request) {
	if err := a.reset.Start(); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, a.reset.Progress(), "succeed")
}

// resetProgress returns the in-memory progress of a running or finished reset.
func (a *systemApi) resetProgress(w http.ResponseWriter, r *http.Request) {
	controllers.SendResult(w, a.reset.Progress(), "succeed")
}

// hostBoundProtectors are the protectors whose key cannot be unwrapped on another machine,
// so exporting a recovery escrow matters most for them.
var hostBoundProtectors = map[string]bool{"dpapi": true, "systemd-creds": true}

// recoveryState reports whether encryption-at-rest is on, which protector wraps the key,
// and whether that protector is host-bound (so the UI can nudge the operator to export a
// recovery escrow before they can't).
func (a *systemApi) recoveryState(w http.ResponseWriter, r *http.Request) {
	enabled := a.keystore != nil && a.keystore.Enabled()
	protector := ""
	if enabled {
		protector = a.keystore.Protector()
	}
	controllers.SendResult(w, map[string]any{
		"enabled":   enabled,
		"protector": protector,
		"hostBound": hostBoundProtectors[protector],
	}, "succeed")
}

// recoveryExport wraps the current master key with an operator passphrase and returns the
// recovery blob as base64 for the browser to download. Admin-gated by the router. The blob
// is only as strong as the passphrase, so a short one is refused.
func (a *systemApi) recoveryExport(w http.ResponseWriter, r *http.Request) {
	if a.keystore == nil || !a.keystore.Enabled() {
		controllers.SendError(w, controllers.ErrBadRequest, "encryption-at-rest is not enabled")
		return
	}
	var body struct {
		Passphrase string `json:"passphrase"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid request body")
		return
	}
	if len(strings.TrimSpace(body.Passphrase)) < 8 {
		controllers.SendError(w, controllers.ErrBadRequest, "passphrase must be at least 8 characters")
		return
	}
	blob, err := a.keystore.ExportEscrow(body.Passphrase)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{
		"filename": fmt.Sprintf("mymatasan-recovery-%s.atrestkey", time.Now().Format("20060102")),
		"keyBase64": base64.StdEncoding.EncodeToString(blob),
	}, "succeed")
}

// recoveryVerify confirms a previously exported recovery file unwraps with the supplied
// passphrase and reports whether it protects the key currently in use. Read-only.
func (a *systemApi) recoveryVerify(w http.ResponseWriter, r *http.Request) {
	if a.keystore == nil || !a.keystore.Enabled() {
		controllers.SendError(w, controllers.ErrBadRequest, "encryption-at-rest is not enabled")
		return
	}
	var body struct {
		Passphrase string `json:"passphrase"`
		KeyBase64  string `json:"keyBase64"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid request body")
		return
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(body.KeyBase64))
	if err != nil || len(data) == 0 {
		controllers.SendError(w, controllers.ErrBadRequest, "recovery file is not valid base64")
		return
	}
	matches, verr := a.keystore.VerifyEscrow(body.Passphrase, data)
	if verr != nil {
		// A wrong passphrase or wrong-format file is a client error, not a server fault.
		controllers.SendResult(w, map[string]any{"valid": false, "matchesCurrent": false, "error": verr.Error()}, "succeed")
		return
	}
	controllers.SendResult(w, map[string]any{"valid": true, "matchesCurrent": matches}, "succeed")
}
