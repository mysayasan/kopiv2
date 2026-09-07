package apis

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myidsan/manual"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
)

// NewManualApi registers the built-in user manual.
//
// Registered on the bare router with NO auth middleware. On this app that is not a convenience,
// it is the point: myidsan is the server nobody can sign in to when it is misconfigured, and
// every question the sign-in screen raises — where the bootstrap password is, why an account has
// no role, why enrolment is being demanded — is asked by somebody who is not authenticated. A
// manual behind the session cookie would be missing at exactly the moment it is wanted.
//
// What that exposes is shipped documentation compiled into the binary: no runtime state, no
// per-user data, nothing an operator has typed. The shared rate limiter still applies.
func NewManualApi(router *mux.Router) {
	h := sharedapis.NewManualHandlers(manual.Library)
	g := router.PathPrefix("/manual").Subrouter()
	g.HandleFunc("", h.List).Methods("GET")
	g.HandleFunc("/bundle", h.Bundle).Methods("GET")
	g.HandleFunc("/search", h.Search).Methods("GET")
	g.HandleFunc("/assets/{name}", func(w http.ResponseWriter, r *http.Request) {
		h.Asset(w, r, mux.Vars(r)["name"])
	}).Methods("GET")
	// Registered last so "bundle" and "assets" are matched by their own routes first.
	g.HandleFunc("/{slug}", func(w http.ResponseWriter, r *http.Request) {
		h.Get(w, r, mux.Vars(r)["slug"])
	}).Methods("GET")
}
