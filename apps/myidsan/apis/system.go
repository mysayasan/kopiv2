package apis

import (
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

// restarter gracefully relaunches the running process. Satisfied by apphost.Restarter.
type restarter interface {
	Restart(reason string)
}

type systemApi struct {
	restarter restarter
}

// NewSystemApi mounts system-level operations under /system. It exists for the Settings
// editor: every block that editor can change is read by the shared host only at boot, so a
// save reports needsRestart and this is how the operator applies it. Without it the page
// could tell an operator a restart was required and offer no way to perform one.
//
// Superadmin-gated as middleware on the whole group, matching every other sensitive myidsan
// surface (settings, audit, backup, mfa-admin). restart is never delegated through the
// permission matrix: it drops every in-flight request on the identity provider the rest of
// the suite authenticates against.
//
//	POST /api/system/restart  — relaunch the process so pending config changes take effect
//
// restart may be nil when the host provided no restarter, in which case the endpoint
// reports unavailable rather than silently doing nothing.
func NewSystemApi(router *mux.Router, auth middlewares.AuthMidware, access *middlewares.AccessSessionMidware, restart restarter) {
	h := &systemApi{restarter: restart}

	g := router.PathPrefix("/system").Subrouter()
	g.Use(auth.Middleware)
	g.Use(access.Middleware)
	g.Use(access.RequireSuperadmin)

	g.HandleFunc("/restart", h.restart).Methods("POST")
}

// restart relaunches the process so changes that are only read at startup take effect. The
// HTTP response is sent BEFORE the relaunch so the client can begin polling for the server
// to come back — a restart that tore down the listener first would look like a failure.
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
