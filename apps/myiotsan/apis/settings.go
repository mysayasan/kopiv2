package apis

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myiotsan/services"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

// NewSettingsApi registers user and role management.
//
// This closes a real gap. myiotsan's policy catalog has named /api/settings/users and
// /api/settings/roles since P0, and the three roles have existed since then — but nothing
// served those routes, so viewer and operator were UNASSIGNABLE and the appliance was
// effectively single-admin. A catalog that names routes the app does not serve is a lie an
// operator would rely on, which is exactly the thing the catalog exists to prevent.
//
// It runs on the shared appliance user service (domain/shared/services), the same code
// mymatasan uses — so bcrypt, sessions and the last-admin guard are one implementation, not two.
type settingsApi struct {
	users sharedservices.ILocalUserService
	roles sharedservices.IAccessRoleService
	notif *services.NotificationSettingsService
	telem *services.TelemetrySettingsService
}

func NewSettingsApi(
	router *mux.Router,
	users sharedservices.ILocalUserService,
	roles sharedservices.IAccessRoleService,
	notif *services.NotificationSettingsService,
	telem *services.TelemetrySettingsService,
) {
	h := &settingsApi{users: users, roles: roles, notif: notif, telem: telem}
	g := router.PathPrefix("/settings").Subrouter()
	g.HandleFunc("/roles", h.listRoles).Methods("GET")
	g.HandleFunc("/users", h.listUsers).Methods("GET")
	g.HandleFunc("/users", h.createUser).Methods("POST")
	g.HandleFunc("/users/{id}", h.updateUser).Methods("PUT")
	g.HandleFunc("/users/{id}", h.deleteUser).Methods("DELETE")
	g.HandleFunc("/users/{id}/password", h.resetPassword).Methods("POST")
	// Outbound notification delivery (per-destination) and the storage/broker knobs.
	g.HandleFunc("/notification", h.getNotification).Methods("GET")
	g.HandleFunc("/notification", h.saveNotification).Methods("PUT")
	g.HandleFunc("/notification/destination", h.saveDestination).Methods("PUT")
	g.HandleFunc("/notification/destination/{id}", h.deleteDestination).Methods("DELETE")
	g.HandleFunc("/notification/test", h.testNotification).Methods("POST")
	g.HandleFunc("/telemetry", h.getTelemetry).Methods("GET")
	g.HandleFunc("/telemetry", h.saveTelemetry).Methods("PUT")
}

// requireAdmin is a self-gate on top of the matrix. The matrix already denies these routes to
// viewer and operator; this is defence in depth on the one surface that can mint an account.
func (a *settingsApi) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	user, ok := sharedapis.LocalUserFromContext(r.Context())
	if !ok || !user.IsAdmin {
		controllers.SendError(w, controllers.ErrLimitedAccess, "administrators only")
		return false
	}
	return true
}

// listRoles returns the roles an admin may assign: viewer, operator, administrator.
func (a *settingsApi) listRoles(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	roles, err := a.roles.List(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, roles, "succeed")
}

func (a *settingsApi) listUsers(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	limit, offset := readPaging(r)
	if limit == 0 {
		limit = 100
	}
	users, total, err := a.users.Get(r.Context(), limit, offset)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"items": users, "total": total}, "succeed")
}

func (a *settingsApi) createUser(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	var body sharedservices.CreateLocalUserRequest
	if !decode(w, r, &body) {
		return
	}
	user, err := a.users.Create(r.Context(), body)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, user, "succeed")
}

func (a *settingsApi) updateUser(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	id, ok := readID(w, r)
	if !ok {
		return
	}
	var body sharedservices.UpdateLocalUserRequest
	if !decode(w, r, &body) {
		return
	}
	// The shared service refuses an edit that would remove the last administrator — an
	// appliance nobody can administer is a bricked appliance.
	user, err := a.users.Update(r.Context(), uint64(id), body)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, user, "succeed")
}

func (a *settingsApi) deleteUser(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	id, ok := readID(w, r)
	if !ok {
		return
	}
	if _, err := a.users.Delete(r.Context(), uint64(id)); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"deleted": id}, "succeed")
}

func (a *settingsApi) resetPassword(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	id, ok := readID(w, r)
	if !ok {
		return
	}
	var body sharedservices.ResetLocalUserPasswordRequest
	if !decode(w, r, &body) {
		return
	}
	user, err := a.users.ResetPassword(r.Context(), uint64(id), body.Password)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, user, "succeed")
}

// --- outbound notification delivery ---------------------------------------------------------

func (a *settingsApi) getNotification(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	settings, err := a.notif.Get(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, settings, "succeed")
}

func (a *settingsApi) saveNotification(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	var body services.NotificationSettings
	if !decode(w, r, &body) {
		return
	}
	settings, err := a.notif.Save(r.Context(), body)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, settings, "succeed")
}

// saveDestination upserts one destination against the persisted settings, so saving one never
// clobbers another. Returns the persisted destination plus the full settings.
func (a *settingsApi) saveDestination(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	var body services.NotificationDestination
	if !decode(w, r, &body) {
		return
	}
	dest, settings, err := a.notif.SaveDestination(r.Context(), body)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"destination": dest, "settings": settings}, "succeed")
}

func (a *settingsApi) deleteDestination(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	id := mux.Vars(r)["id"]
	if id == "" {
		controllers.SendError(w, controllers.ErrBadRequest, "a destination id is required")
		return
	}
	settings, err := a.notif.DeleteDestination(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, settings, "succeed")
}

func (a *settingsApi) testNotification(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	// Severity is an optional query param so the button needs no body; Test defaults it.
	if err := a.notif.Test(r.Context(), r.URL.Query().Get("severity")); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"sent": true}, "succeed")
}

// --- storage / broker knobs (restart-to-apply) ----------------------------------------------

func (a *settingsApi) getTelemetry(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	settings, err := a.telem.Get(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, settings, "succeed")
}

func (a *settingsApi) saveTelemetry(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	var body services.TelemetrySettings
	if !decode(w, r, &body) {
		return
	}
	settings, err := a.telem.Save(r.Context(), body)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, settings, "succeed")
}
