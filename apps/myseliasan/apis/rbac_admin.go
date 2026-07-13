package apis

import (
	"fmt"
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
	audit   services.IAuditService
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
	roles sharedservices.IAccessRoleService, users services.IControlUserService, audit services.IAuditService) {
	h := &rbacAdminApi{roles: roles, users: users, session: session, audit: audit}
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
	// A superadmin may not change their OWN role — no self-elevation/self-demotion.
	if id == operatorUserId(r) {
		controllers.SendError(w, controllers.ErrLimitedAccess, "you cannot change your own role")
		return
	}
	var body struct {
		RoleId int64 `json:"roleId"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	// roleId 0 = revoke the role (back to pending / no access). Any other id must exist.
	if body.RoleId != 0 {
		if role, err := a.roles.GetById(r.Context(), body.RoleId); err != nil || role == nil {
			controllers.SendError(w, controllers.ErrBadRequest, "unknown role")
			return
		}
	}
	// Capture the prior role so the audit trail records the before/after transition.
	var prevRole int64 = -1
	if target, terr := a.users.GetById(r.Context(), id); terr == nil && target != nil {
		prevRole = target.RoleId
	}
	if err := a.users.SetRole(r.Context(), id, body.RoleId); err != nil {
		a.recordUserAction(r, "rbac.set_role", id, "error", "set role failed: "+err.Error(), nil)
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	a.recordUserAction(r, "rbac.set_role", id, "success",
		fmt.Sprintf("changed user %d role %d → %d", id, prevRole, body.RoleId),
		map[string]any{"prevRoleId": prevRole, "newRoleId": body.RoleId})
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
	// Guard against lockout: don't let the stock superadmin be disabled until a real
	// superadmin is active to take over.
	if body.Disabled {
		if target, err := a.users.GetById(r.Context(), id); err == nil && target != nil && target.IsStock {
			if _, realActive, serr := a.users.SuperadminStatus(r.Context()); serr == nil && !realActive {
				controllers.SendError(w, controllers.ErrBadRequest, "promote a real superadmin before disabling the stock account")
				return
			}
		}
	}
	if err := a.users.SetDisabled(r.Context(), id, body.Disabled); err != nil {
		a.recordUserAction(r, "rbac.set_disabled", id, "error", "set disabled failed: "+err.Error(), nil)
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	verb := "enabled"
	if body.Disabled {
		verb = "disabled"
	}
	a.recordUserAction(r, "rbac.set_disabled", id, "success",
		fmt.Sprintf("%s user %d", verb, id), map[string]any{"disabled": body.Disabled})
	controllers.SendResult(w, map[string]any{"ok": true}, "succeed")
}

// elevateUser is the bootstrap handoff: promote one chosen user to superadmin, then
// retire every stock account. The retired stock account is forced out on its next
// request (the session middleware rejects a disabled user).
func (a *rbacAdminApi) elevateUser(w http.ResponseWriter, r *http.Request) {
	id := pathInt64(r, "id")
	// A superadmin may not elevate themselves (no-op at best; blocks self-targeting).
	if id == operatorUserId(r) {
		controllers.SendError(w, controllers.ErrLimitedAccess, "choose another user to elevate")
		return
	}
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
		a.recordUserAction(r, "rbac.elevate", id, "error", "elevate failed: "+err.Error(), nil)
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	a.recordUserAction(r, "rbac.elevate", id, "success",
		fmt.Sprintf("elevated user %q (id %d) to superadmin", target.Email, id),
		map[string]any{"superadminRoleId": super.Id})
	// The stock account is intentionally left active: instead of auto-retiring it (and
	// risking lockout), the SPA shows a persistent banner prompting the operator to
	// disable it from the Users list once they're satisfied with the new superadmin.
	controllers.SendResult(w, map[string]any{
		"ok":      true,
		"warning": "Superadmin granted. The stock superadmin is still active — disable it from the Users list to finish the handoff.",
	}, "succeed")
}

// recordUserAction audits an RBAC change targeting a control-plane user.
func (a *rbacAdminApi) recordUserAction(r *http.Request, action string, targetUserID int64, outcome, detail string, meta map[string]any) {
	if a.audit == nil {
		return
	}
	actorID, actorLabel, roleID := auditActor(r)
	a.audit.Record(r.Context(), services.AuditEntry{
		Action:     action,
		ActorId:    actorID,
		ActorEmail: actorLabel,
		ActorRole:  roleID,
		TargetType: "user",
		TargetId:   strconv.FormatInt(targetUserID, 10),
		Outcome:    outcome,
		Detail:     detail,
		Metadata:   meta,
		ClientIp:   clientIP(r),
	})
}

func pathInt64(r *http.Request, key string) int64 {
	v, _ := strconv.ParseInt(mux.Vars(r)[key], 10, 64)
	return v
}
