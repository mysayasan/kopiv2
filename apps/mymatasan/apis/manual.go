package apis

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mymatasan/manual"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
)

// NewManualApi registers the built-in user manual.
//
// It is mounted on the PUBLIC router, before the auth middleware, and that is a deliberate
// decision rather than an oversight. Two of the places a reader most needs the manual — the
// sign-in screen and the first-run wizard's earliest steps — are places they either cannot
// authenticate yet or are stuck trying to. A manual you must already be signed in to read is
// missing at exactly the moment it is wanted.
//
// What that exposes is shipped documentation compiled into the binary: no runtime state, no
// per-user data, no configuration values, nothing an operator has typed. The same appliance
// already serves an unauthenticated sign-in page that names the product.
//
// Keeping it off the matrix has a second benefit worth stating: EnsureApplianceRoles seeds a
// role's permissions only when that role has none, so a rule added to the policy catalog today
// never reaches an install that already has its matrix. A manual behind the matrix would be
// readable on new installs and quietly denied to every viewer on every upgraded one.
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
