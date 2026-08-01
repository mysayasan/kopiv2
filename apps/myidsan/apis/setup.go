package apis

import (
	"net/http"

	"github.com/gorilla/mux"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

// NewSetupApi exposes the first-run setup wizard's completion flag. State is
// readable by any signed-in user (the SPA checks it right after login);
// completing is a write and goes through the RBAC matrix — the wizard only ever
// runs as the bootstrap superadmin, who bypasses the matrix.
func NewSetupApi(router *mux.Router, auth middlewares.AuthMidware, access *middlewares.AccessSessionMidware, setup sharedservices.ISetupStateService) {
	h := sharedapis.NewSetupHandlers(setup)
	g := router.PathPrefix("/setup").Subrouter()
	g.Handle("/state", auth.Middleware(http.HandlerFunc(h.State))).Methods("GET")

	completeGroup := router.PathPrefix("/setup/complete").Subrouter()
	completeGroup.Use(auth.Middleware)
	completeGroup.Use(access.Middleware)
	completeGroup.HandleFunc("", h.Complete).Methods("POST")
}
