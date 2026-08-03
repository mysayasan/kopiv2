package apis

import (
	"net/http"
	"time"

	"github.com/gorilla/mux"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

// restarter gracefully relaunches the running process. Satisfied by apphost.Restarter.
type restarter interface {
	Restart(reason string)
}

type systemApi struct {
	session   *middlewares.AccessSessionMidware
	restarter restarter
	reset     *sharedservices.SystemResetService
}

// NewSystemApi mounts system-level operations under /system. The restart POST is
// superadmin-gated: it is how the operator applies settings changes, which every editable
// block requires (the shared host reads them only at boot). restarter may be nil if the
// host did not provide one, in which case restart reports unavailable.
//
//	POST /api/system/restart        — relaunch the process so pending config changes take effect
//	GET  /api/system/reset/state    — whether factory reset is available, and the phrase to type
//	POST /api/system/reset          — start a factory reset (body: {"confirm": "<phrase>"})
//	GET  /api/system/reset/progress — in-memory progress of a running reset
//
// Every reset route is superadmin-gated like the restart, and the POST additionally
// requires the typed confirmation to match server-side.
func NewSystemApi(router *mux.Router, auth middlewares.AuthMidware, session *middlewares.AccessSessionMidware, restart restarter, reset *sharedservices.SystemResetService) {
	h := &systemApi{session: session, restarter: restart, reset: reset}
	g := router.PathPrefix("/system").Subrouter()
	g.Use(auth.Middleware)
	g.Use(session.Middleware)
	g.HandleFunc("/restart", h.requireSuper(h.restart)).Methods("POST")

	if reset != nil {
		rh := sharedapis.NewSystemResetHandlers(reset)
		g.HandleFunc("/reset/state", h.requireSuper(rh.State)).Methods("GET")
		g.HandleFunc("/reset/progress", h.requireSuper(rh.Progress)).Methods("GET")
		g.HandleFunc("/reset", h.requireSuper(rh.Start)).Methods("POST")
	}
}

func (a *systemApi) requireSuper(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.session.IsSuperadmin(r) {
			controllers.SendError(w, controllers.ErrLimitedAccess, "superadmin access required")
			return
		}
		next(w, r)
	}
}

// restart relaunches the process so changes that are only read at startup take effect. The
// HTTP response is sent first; the relaunch happens a moment later so the client can begin
// polling for the server to come back.
func (a *systemApi) restart(w http.ResponseWriter, r *http.Request) {
	if a.restarter == nil {
		controllers.SendError(w, controllers.ErrInternalServerError, "restart is not available")
		return
	}
	controllers.SendResult(w, map[string]any{"restarting": true}, "succeed")
	go func() {
		time.Sleep(500 * time.Millisecond)
		a.restarter.Restart("settings change: api restart request")
	}()
}
