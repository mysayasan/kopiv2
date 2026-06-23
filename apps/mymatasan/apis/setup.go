package apis

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

type setupApi struct {
	setup services.ISetupStateService
}

// NewSetupApi registers the first-run setup state endpoints: GET /setup/state
// (read-only) and POST /setup/complete (admin — a write, so non-admins are
// blocked by NewRequireAdminForWrites).
func NewSetupApi(router *mux.Router, setup services.ISetupStateService) {
	h := &setupApi{setup: setup}
	g := router.PathPrefix("/setup").Subrouter()
	g.HandleFunc("/state", h.state).Methods("GET")
	g.HandleFunc("/complete", h.complete).Methods("POST")
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
