package apis

import (
	"net/http"

	"github.com/gorilla/mux"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

type setupApi struct {
	handlers *sharedapis.SetupHandlers
}

// NewSetupApi registers the first-run wizard's completion flag: GET /setup/state
// (any signed-in user — the SPA checks it right after login) and POST /setup/complete.
//
// Dismissal used to live in localStorage, which made it per-BROWSER: the same admin
// signing in from a second machine, or after clearing site data, met the wizard again on
// a hub that had been running for months. It is a property of the INSTALL, so it belongs
// on the server — the same `setup.state` contract the other three apps use.
func NewSetupApi(router *mux.Router, setup sharedservices.ISetupStateService) {
	h := &setupApi{handlers: sharedapis.NewSetupHandlers(setup)}
	g := router.PathPrefix("/setup").Subrouter()
	g.HandleFunc("/state", h.handlers.State).Methods("GET")
	g.HandleFunc("/complete", h.complete).Methods("POST")
}

// complete is admin-gated on top of the matrix, matching the settings surface: the wizard
// is only ever shown to an admin, so an operator dismissing it for the whole install
// would be dismissing something they were never offered.
func (a *setupApi) complete(w http.ResponseWriter, r *http.Request) {
	user, ok := sharedapis.LocalUserFromContext(r.Context())
	if !ok || !user.IsAdmin {
		controllers.SendError(w, controllers.ErrLimitedAccess, "administrators only")
		return
	}
	a.handlers.Complete(w, r)
}
