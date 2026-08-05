package apis

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mypintusan/services"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

type settingsApi struct {
	settings services.IAccessSettingsService
}

// NewSettingsApi exposes the runtime settings — the screen that makes this app configurable by a
// facilities manager instead of by somebody editing JSON over SSH.
//
// config.json only ever SEEDS the first boot; from then on these endpoints are the way anything
// changes. That is mymatasan's established pattern, and it is what the "zero prior knowledge"
// product promise actually requires.
func NewSettingsApi(router *mux.Router, settings services.IAccessSettingsService) {
	a := &settingsApi{settings: settings}
	g := router.PathPrefix("/settings/access").Subrouter()
	g.HandleFunc("", a.get).Methods("GET")
	g.HandleFunc("", a.save).Methods("PUT")
	g.HandleFunc("/reset", a.reset).Methods("POST")
}

// get returns the live settings. Site keys are redacted by the entity's own marshaller, so this
// handler cannot leak one by forgetting to.
func (a *settingsApi) get(w http.ResponseWriter, r *http.Request) {
	out, err := a.settings.Get(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, out)
}

// save validates and persists an edit.
//
// Admin-only on top of the matrix: these values decide which readers are polled, which are
// encrypted, and how a door behaves. They are not operator settings — a wrong entry here does not
// produce a bad reading, it produces a door that opens for the wrong person or an alarm that never
// comes.
func (a *settingsApi) save(w http.ResponseWriter, r *http.Request) {
	user, ok := sharedapis.LocalUserFromContext(r.Context())
	if !ok || !user.IsAdmin {
		controllers.SendError(w, controllers.ErrLimitedAccess, "administrators only")
		return
	}

	var body services.AccessSettings
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}

	out, err := a.settings.Save(r.Context(), body, user.Id)
	if err != nil {
		// A validation failure is the USER'S problem to fix, and the message says what is wrong in
		// plain language ("two readers at address 1"), because the person reading it is the one who
		// has to go and change a DIP switch.
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, out)
}

// reset restores the config.json seed.
//
// This is the recovery path for an edit that stopped the controller working. Without it, a
// mistyped timezone would need somebody with database access — which on a door appliance sold to a
// facilities team means a site visit.
func (a *settingsApi) reset(w http.ResponseWriter, r *http.Request) {
	user, ok := sharedapis.LocalUserFromContext(r.Context())
	if !ok || !user.IsAdmin {
		controllers.SendError(w, controllers.ErrLimitedAccess, "administrators only")
		return
	}
	out, err := a.settings.Reset(r.Context(), user.Id)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, out)
}
