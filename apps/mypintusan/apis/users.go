package apis

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

// NewUserApi registers user and role management.
//
// THIS CLOSES A GAP THAT MADE THE APP'S ENTIRE ROLE MODEL UNREACHABLE. services/rbac.go seeds
// three roles on every boot and spends thirty lines reasoning about the line between them — "who
// may change the rules about who gets in", credentials operator-level, grants and lockdown
// admin-only. EnsureRoles created viewer and operator on every install. And nothing served a route
// that could put a person in one of them: there was no user API and no Users screen, so a
// mypintusan appliance had exactly one account, the bootstrap admin, and no way to make a second.
// Every access decision the catalog draws was theoretical.
//
// myiotsan had the identical gap and closed it the same way; this runs on the same shared
// appliance user service (domain/shared/services), so bcrypt, sessions and the last-admin guard
// are one implementation rather than five.
type userApi struct {
	users sharedservices.ILocalUserService
	roles sharedservices.IAccessRoleService
}

func NewUserApi(router *mux.Router, users sharedservices.ILocalUserService, roles sharedservices.IAccessRoleService) {
	h := &userApi{users: users, roles: roles}
	g := router.PathPrefix("/settings").Subrouter()
	g.HandleFunc("/roles", h.listRoles).Methods("GET")
	g.HandleFunc("/users", h.listUsers).Methods("GET")
	g.HandleFunc("/users", h.createUser).Methods("POST")
	g.HandleFunc("/users/{id:[0-9]+}", h.updateUser).Methods("PUT")
	g.HandleFunc("/users/{id:[0-9]+}", h.deleteUser).Methods("DELETE")
	g.HandleFunc("/users/{id:[0-9]+}/password", h.resetPassword).Methods("POST")
}

// requireAdmin is a self-gate on top of the matrix. The matrix already denies these routes to
// viewer and operator; this is defence in depth on the one surface that can mint an account with
// any power on the appliance, including the power to open every door.
func (a *userApi) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	user, ok := sharedapis.LocalUserFromContext(r.Context())
	if !ok || !user.IsAdmin {
		controllers.SendError(w, controllers.ErrLimitedAccess, "administrators only")
		return false
	}
	return true
}

func decodeBody(w http.ResponseWriter, r *http.Request, into any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	if err := json.NewDecoder(r.Body).Decode(into); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return false
	}
	return true
}

func userID(w http.ResponseWriter, r *http.Request) (uint64, bool) {
	id, err := strconv.ParseUint(mux.Vars(r)["id"], 10, 64)
	if err != nil || id == 0 {
		controllers.SendError(w, controllers.ErrBadRequest, "bad user id")
		return 0, false
	}
	return id, true
}

// listRoles returns the roles an admin may assign: viewer, operator, administrator.
func (a *userApi) listRoles(w http.ResponseWriter, r *http.Request) {
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

func (a *userApi) listUsers(w http.ResponseWriter, r *http.Request) {
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

func (a *userApi) createUser(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	var body sharedservices.CreateLocalUserRequest
	if !decodeBody(w, r, &body) {
		return
	}
	user, err := a.users.Create(r.Context(), body)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, user, "succeed")
}

func (a *userApi) updateUser(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	id, ok := userID(w, r)
	if !ok {
		return
	}
	var body sharedservices.UpdateLocalUserRequest
	if !decodeBody(w, r, &body) {
		return
	}
	// The shared service refuses an edit that would remove the last administrator. On a door
	// controller that matters more than usual: an appliance nobody can administer is one where
	// nobody can lift a lockdown.
	user, err := a.users.Update(r.Context(), id, body)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, user, "succeed")
}

func (a *userApi) deleteUser(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	id, ok := userID(w, r)
	if !ok {
		return
	}
	if _, err := a.users.Delete(r.Context(), id); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"deleted": id}, "succeed")
}

func (a *userApi) resetPassword(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	id, ok := userID(w, r)
	if !ok {
		return
	}
	var body sharedservices.ResetLocalUserPasswordRequest
	if !decodeBody(w, r, &body) {
		return
	}
	user, err := a.users.ResetPassword(r.Context(), id, body.Password)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, user, "succeed")
}
