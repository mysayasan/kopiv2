package apis

import (
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myseliasan/services"
	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

type rbacAdminApi struct {
	roles   sharedservices.IAccessRoleService
	users   services.IControlUserService
	session *middlewares.AccessSessionMidware
}

// NewRbacAdminApi mounts the superadmin-only, myseliasan-specific user-management
// surface plus the bootstrap handoff. Role + permission-matrix management is the
// shared accessrbac module (/api/access-rbac), so it is NOT duplicated here. Every
// handler self-gates to superadmin regardless of the permission matrix.
//
//	GET    /api/rbac/users                 — list control-plane users
//	POST   /api/rbac/users/{id}/role       — {roleId} reassign a user's role
//	POST   /api/rbac/users/{id}/disabled   — {disabled} enable/disable a user
//	POST   /api/rbac/users/{id}/elevate    — make superadmin + retire stock (handoff)
func NewRbacAdminApi(router *mux.Router, auth middlewares.AuthMidware, session *middlewares.AccessSessionMidware,
	roles sharedservices.IAccessRoleService, users services.IControlUserService) {
	h := &rbacAdminApi{roles: roles, users: users, session: session}
	g := router.PathPrefix("/rbac").Subrouter()
	g.Use(auth.Middleware)
	g.Use(session.Middleware) // disabled/must-change gate; superadmin bypasses the matrix

	g.HandleFunc("/users", h.requireSuper(h.listUsers)).Methods("GET")
	g.HandleFunc("/users/{id}/role", h.requireSuper(h.setUserRole)).Methods("POST")
	g.HandleFunc("/users/{id}/disabled", h.requireSuper(h.setUserDisabled)).Methods("POST")
	g.HandleFunc("/users/{id}/elevate", h.requireSuper(h.elevateUser)).Methods("POST")
}

// requireSuper wraps a handler so only a superadmin session may call it.
func (a *rbacAdminApi) requireSuper(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.session.IsSuperadmin(r) {
			controllers.SendError(w, controllers.ErrLimitedAccess, "superadmin access required")
			return
		}
		next(w, r)
	}
}

func (a *rbacAdminApi) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := a.users.List(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, users, "succeed")
}

func (a *rbacAdminApi) setUserRole(w http.ResponseWriter, r *http.Request) {
	id := pathInt64(r, "id")
	var body struct {
		RoleId int64 `json:"roleId"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	if role, err := a.roles.GetById(r.Context(), body.RoleId); err != nil || role == nil {
		controllers.SendError(w, controllers.ErrBadRequest, "unknown role")
		return
	}
	if err := a.users.SetRole(r.Context(), id, body.RoleId); err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"ok": true}, "succeed")
}

func (a *rbacAdminApi) setUserDisabled(w http.ResponseWriter, r *http.Request) {
	id := pathInt64(r, "id")
	var body struct {
		Disabled bool `json:"disabled"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	if err := a.users.SetDisabled(r.Context(), id, body.Disabled); err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"ok": true}, "succeed")
}

// elevateUser is the bootstrap handoff: promote one chosen user to superadmin, then
// retire every stock account. The retired stock account is forced out on its next
// request (the session middleware rejects a disabled user).
func (a *rbacAdminApi) elevateUser(w http.ResponseWriter, r *http.Request) {
	id := pathInt64(r, "id")
	super, err := a.roles.GetByName(r.Context(), sharedservices.RoleSuperadmin)
	if err != nil || super == nil {
		controllers.SendError(w, controllers.ErrInternalServerError, "superadmin role missing")
		return
	}
	target, err := a.users.GetById(r.Context(), id)
	if err != nil || target == nil {
		controllers.SendError(w, controllers.ErrBadRequest, "unknown user")
		return
	}
	if target.IsStock {
		controllers.SendError(w, controllers.ErrBadRequest, "choose a real (non-stock) user to elevate")
		return
	}
	if target.Disabled {
		controllers.SendError(w, controllers.ErrBadRequest, "cannot elevate a disabled user")
		return
	}
	if err := a.users.SetRole(r.Context(), id, super.Id); err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	retired, err := a.users.RetireStock(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{
		"ok":      true,
		"retired": retired,
		"warning": "The stock superadmin has been retired and signed out. From now on, administer this control plane with the elevated account.",
	}, "succeed")
}

func pathInt64(r *http.Request, key string) int64 {
	v, _ := strconv.ParseInt(mux.Vars(r)[key], 10, 64)
	return v
}
