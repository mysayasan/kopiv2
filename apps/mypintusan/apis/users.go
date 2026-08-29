package apis

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mypintusan/services"
	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
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
	// audit matters more here than anywhere else in the app: this surface can mint an account that
	// holds every other power on the appliance, including the power to open every door and to
	// change who else may. An account created at 02:00 and deleted at 02:20 leaves nothing behind
	// but the doors it opened in between.
	audit *Auditor
}

func NewUserApi(router *mux.Router, users sharedservices.ILocalUserService, roles sharedservices.IAccessRoleService, audit *Auditor) {
	h := &userApi{users: users, roles: roles, audit: audit}
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

// lookup finds one account so the trail can say what an edit or a deletion actually changed.
//
// It pages rather than fetching by id because the shared user service exposes no by-id read, and
// adding one to a service four apps depend on is not this feature's business. The scan is bounded
// and the cost is fine for what this is: a door appliance has a handful of accounts, not a
// directory, and the alternative is an audit entry that names a user id and nothing else.
//
// A miss is not an error — the entry is still written, just with less in it. An unattributed record
// beats a missing one, and the whole point of this trail is that nothing prevents it being written.
func (a *userApi) lookup(r *http.Request, id uint64) *sharedentities.LocalUser {
	users, _, err := a.users.Get(r.Context(), 500, 0)
	if err != nil {
		return nil
	}
	for _, u := range users {
		if u != nil && uint64(u.Id) == id {
			return u
		}
	}
	return nil
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
	// The ROLE is the payload. "A user was created" is administrative noise; "a user was created as
	// an administrator" is somebody handing out the keys to the building.
	a.audit.Success(r, services.ActionUserCreate, services.TargetUser, ID(user.Id),
		fmt.Sprintf("account %q created with role %d", user.Username, user.RoleId),
		map[string]any{"username": user.Username, "displayName": user.DisplayName,
			"roleId": user.RoleId, "isActive": user.IsActive})
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
	// The BEFORE state is read for the trail — a role change is the edit worth catching, and the
	// response alone cannot show what the role used to be.
	prevRole, prevActive := int64(0), false
	if existing := a.lookup(r, id); existing != nil {
		prevRole, prevActive = existing.RoleId, existing.IsActive
	}
	// The shared service refuses an edit that would remove the last administrator. On a door
	// controller that matters more than usual: an appliance nobody can administer is one where
	// nobody can lift a lockdown.
	user, err := a.users.Update(r.Context(), id, body)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	detail := fmt.Sprintf("account %q updated", user.Username)
	if prevRole != user.RoleId {
		detail = fmt.Sprintf("account %q moved from role %d to role %d", user.Username, prevRole, user.RoleId)
	}
	a.audit.Success(r, services.ActionUserUpdate, services.TargetUser, ID(user.Id), detail,
		map[string]any{"username": user.Username,
			"roleId": map[string]any{"from": prevRole, "to": user.RoleId},
			"active": map[string]any{"from": prevActive, "to": user.IsActive}})
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
	// Read before the delete: afterwards there is nothing left to name, and "user 4 deleted" is the
	// entry that makes an investigation give up.
	gone := a.lookup(r, id)
	if _, err := a.users.Delete(r.Context(), id); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	name, role := fmt.Sprintf("user %d", id), int64(0)
	if gone != nil {
		name, role = gone.Username, gone.RoleId
	}
	a.audit.Success(r, services.ActionUserDelete, services.TargetUser, strconv.FormatUint(id, 10),
		fmt.Sprintf("account %q deleted", name),
		map[string]any{"username": name, "roleId": role})
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
	// An administrator setting somebody else's password is a takeover of that account, however
	// legitimate the reason. The password itself is never recorded — the entry says it happened,
	// who did it and to whom, which is the whole of what an investigation can act on.
	a.audit.Success(r, services.ActionUserPassword, services.TargetUser, ID(user.Id),
		fmt.Sprintf("password reset for account %q by an administrator", user.Username),
		map[string]any{"username": user.Username})
	controllers.SendResult(w, user, "succeed")
}
