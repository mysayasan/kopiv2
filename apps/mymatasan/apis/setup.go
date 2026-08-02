package apis

import (
	"github.com/gorilla/mux"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
)

// NewSetupApi registers the first-run setup state endpoints: GET /setup/state
// (read-only) and POST /setup/complete (admin — a write, so non-admins are
// blocked by NewRequireAdminForWrites).
func NewSetupApi(router *mux.Router, setup sharedservices.ISetupStateService) {
	h := sharedapis.NewSetupHandlers(setup)
	g := router.PathPrefix("/setup").Subrouter()
	g.HandleFunc("/state", h.State).Methods("GET")
	g.HandleFunc("/complete", h.Complete).Methods("POST")
}
