package apis

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myidsan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

// setupApi exposes the first-run setup wizard's completion flag. State is
// readable by any signed-in user (the SPA checks it right after login);
// completing is a write and goes through the RBAC matrix — the wizard only ever
// runs as the bootstrap superadmin, who bypasses the matrix.
type setupApi struct {
	setup services.ISetupStateService
}

func NewSetupApi(router *mux.Router, auth middlewares.AuthMidware, access *middlewares.AccessSessionMidware, setup services.ISetupStateService) {
	h := &setupApi{setup: setup}
	g := router.PathPrefix("/setup").Subrouter()
	g.Handle("/state", auth.Middleware(http.HandlerFunc(h.state))).Methods("GET")

	completeGroup := router.PathPrefix("/setup/complete").Subrouter()
	completeGroup.Use(auth.Middleware)
	completeGroup.Use(access.Middleware)
	completeGroup.HandleFunc("", h.complete).Methods("POST")
}

func (a *setupApi) state(w http.ResponseWriter, r *http.Request) {
	state, err := a.setup.Get(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, state, "succeed")
}

func (a *setupApi) complete(w http.ResponseWriter, r *http.Request) {
	state, err := a.setup.Complete(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, state, "succeed")
}
