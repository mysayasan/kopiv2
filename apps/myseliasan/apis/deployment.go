package apis

import (
	"net/http"

	"github.com/gorilla/mux"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

// NewDeploymentApi exposes the deployment-mode question and its readiness
// checklist. Reading is allowed to any signed-in user (the wizard and the
// Settings panel both render it, and it carries no secret — the at-rest row is a
// one-way fingerprint, never the key); declaring the mode is a write and goes
// through the RBAC matrix, like every other configuration change here.
func NewDeploymentApi(
	router *mux.Router,
	auth middlewares.AuthMidware,
	access *middlewares.AccessSessionMidware,
	mode sharedservices.IDeploymentModeService,
	env func() sharedservices.DeploymentEnv,
) {
	h := sharedapis.NewDeploymentHandlers(mode, env)

	g := router.PathPrefix("/deployment").Subrouter()
	g.Handle("/preflight", auth.Middleware(http.HandlerFunc(h.Preflight))).Methods("GET")
	g.Handle("/mode", auth.Middleware(http.HandlerFunc(h.Mode))).Methods("GET")

	modeGroup := router.PathPrefix("/deployment/mode").Subrouter()
	modeGroup.Use(auth.Middleware)
	modeGroup.Use(access.Middleware)
	modeGroup.HandleFunc("", h.SetMode).Methods("POST")
}
