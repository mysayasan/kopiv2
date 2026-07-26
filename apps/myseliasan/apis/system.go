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
	session   *middlewares.AccessSessionMidware
	restarter restarter
}

// NewSystemApi mounts system-level operations under /system. The restart POST is
// superadmin-gated: it is how the operator applies settings changes, which every editable
// block requires (the shared host reads them only at boot). restarter may be nil if the
// host did not provide one, in which case restart reports unavailable.
//
//	POST /api/system/restart  — relaunch the process so pending config changes take effect
func NewSystemApi(router *mux.Router, auth middlewares.AuthMidware, session *middlewares.AccessSessionMidware, restart restarter) {
	h := &systemApi{session: session, restarter: restart}
	g := router.PathPrefix("/system").Subrouter()
	g.Use(auth.Middleware)
	g.Use(session.Middleware)
	g.HandleFunc("/restart", h.requireSuper(h.restart)).Methods("POST")
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
