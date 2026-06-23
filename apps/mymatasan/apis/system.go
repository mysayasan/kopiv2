package apis

import (
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

// restarter gracefully relaunches the running process. Satisfied by apphost.Restarter.
type restarter interface {
	Restart(reason string)
}

type systemApi struct {
	reset     *services.SystemResetService
	restarter restarter
}

// NewSystemApi registers system-level operations under /system. The factory reset and
// restart POSTs are admin-gated (write) by the router's require-admin middleware; reset
// is additionally guarded by bootstrap.allowReset in the service.
func NewSystemApi(router *mux.Router, reset *services.SystemResetService, restart restarter) {
	h := &systemApi{reset: reset, restarter: restart}
	g := router.PathPrefix("/system").Subrouter()
	g.HandleFunc("/reset", h.startReset).Methods("POST")
	g.HandleFunc("/reset/state", h.resetState).Methods("GET")
	g.HandleFunc("/reset/progress", h.resetProgress).Methods("GET")
	g.HandleFunc("/restart", h.restart).Methods("POST")
	g.HandleFunc("/time", h.time).Methods("GET")
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
