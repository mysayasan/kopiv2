package apis

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myseliasan/manual"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
)

// NewManualApi registers the built-in user manual.
//
// Registered on the bare router with NO auth middleware — the same shape as the node self-drop
// and enrolment notices above it. That is deliberate: the sign-in screen is one of the two places
// a reader most needs the manual (the first-run wizard is the other), and both are places they
// cannot authenticate yet. A manual you must already be signed in to read is missing at exactly
// the moment it is wanted.
//
// What that exposes is shipped documentation compiled into the binary: no runtime state, no
// per-user data, nothing an operator has typed. The shared rate limiter still applies.
func NewManualApi(router *mux.Router) {
	h := sharedapis.NewManualHandlers(manual.Library)
	g := router.PathPrefix("/manual").Subrouter()
	g.HandleFunc("", h.List).Methods("GET")
	g.HandleFunc("/bundle", h.Bundle).Methods("GET")
	g.HandleFunc("/assets/{name}", func(w http.ResponseWriter, r *http.Request) {
		h.Asset(w, r, mux.Vars(r)["name"])
	}).Methods("GET")
	// Registered last so "bundle" and "assets" are matched by their own routes first.
	g.HandleFunc("/{slug}", func(w http.ResponseWriter, r *http.Request) {
		h.Get(w, r, mux.Vars(r)["slug"])
	}).Methods("GET")
}
