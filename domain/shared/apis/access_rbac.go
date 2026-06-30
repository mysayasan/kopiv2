package apis

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/domain/entities"
	enumauth "github.com/mysayasan/kopiv2/domain/enums/auth"
	"github.com/mysayasan/kopiv2/domain/models"
	"github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

// accessRbacApi is the shared, superadmin-only management surface for the accessrbac
// core (roles + the per-endpoint permission matrix). It is identical for every app
// that uses the shared module (myseliasan, myidsan, ...), so the "Settings → RBAC"
// page is the same everywhere. User→role assignment stays app-specific (each app has
// its own user store), so it is NOT handled here.
type accessRbacApi struct {
	access *middlewares.AccessSessionMidware
	roles  services.IAccessRoleService
	perms  services.IAccessPermissionService
}

// NewAccessRbacApi mounts the role + permission management endpoints:
//
//	GET    /api/access-rbac/me                    — caller's own role + permission matrix (auth-only)
//	GET    /api/access-rbac/roles                 — list roles
//	POST   /api/access-rbac/roles                 — {name,description} create a role
//	PUT    /api/access-rbac/roles/{id}            — {name,description} update a role
//	DELETE /api/access-rbac/roles/{id}            — delete a (non-builtin) role
//	GET    /api/access-rbac/permissions?roleId=   — list a role's permission rows
//	POST   /api/access-rbac/permissions           — {roleId,path,canGet,...} upsert a row
//	DELETE /api/access-rbac/permissions/{id}      — delete a permission row
//
// Every handler self-gates to a superadmin session regardless of the matrix.
func NewAccessRbacApi(
	router *mux.Router,
	auth middlewares.AuthMidware,
	access *middlewares.AccessSessionMidware,
	roles services.IAccessRoleService,
	perms services.IAccessPermissionService,
) {
	h := &accessRbacApi{access: access, roles: roles, perms: perms}

	// /me returns the caller's OWN role + permission matrix so the SPA can compute
	// menu visibility from the same rules that gate the APIs. It is auth-only (NOT
	// behind the permission matrix) — every authenticated user may read their own
	// authorization state.
	router.Handle("/access-rbac/me", auth.Middleware(http.HandlerFunc(h.me))).Methods("GET")

	g := router.PathPrefix("/access-rbac").Subrouter()
	g.Use(auth.Middleware)
	g.Use(access.Middleware)

	g.HandleFunc("/roles", h.super(h.listRoles)).Methods("GET")
	g.HandleFunc("/roles", h.super(h.createRole)).Methods("POST")
	g.HandleFunc("/roles/{id}", h.super(h.updateRole)).Methods("PUT")
	g.HandleFunc("/roles/{id}", h.super(h.deleteRole)).Methods("DELETE")
	g.HandleFunc("/permissions", h.super(h.listPermissions)).Methods("GET")
	g.HandleFunc("/permissions", h.super(h.upsertPermission)).Methods("POST")
	g.HandleFunc("/permissions/{id}", h.super(h.deletePermission)).Methods("DELETE")
}

func (a *accessRbacApi) super(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.access.IsSuperadmin(r) {
			controllers.SendError(w, controllers.ErrLimitedAccess, "superadmin access required")
			return
		}
		next(w, r)
	}
}

// me reports the caller's role and effective permission rows. A superadmin returns
// isSuperadmin=true with no rows (the SPA treats that as a wildcard); any other role
// returns its matrix so the SPA shows only the menus it has GET access to.
func (a *accessRbacApi) me(w http.ResponseWriter, r *http.Request) {
	principal, err := a.access.CurrentPrincipal(r)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	if principal == nil || principal.Disabled {
		controllers.SendError(w, controllers.ErrPermission, "your session is no longer valid")
		return
	}
	out := map[string]any{
		"roleId":             principal.RoleId,
		"mustChangePassword": principal.MustChangePassword,
		"isSuperadmin":       false,
		// pending = authenticated but no role assigned yet (awaiting admin clearance).
		// The SPA shows an "access pending" screen instead of the app.
		"pending":     true,
		"permissions": []*entities.AccessRolePermission{},
	}
	// Surface the caller's own identity so the SPA can recognize "self" (e.g. to avoid
	// offering to disable your own account).
	if claims, ok := r.Context().Value(enumauth.Claims).(*models.JwtCustomClaims); ok && claims != nil {
		out["userId"] = claims.Id
		out["email"] = claims.Email
	}
	if role, _ := a.roles.GetById(r.Context(), principal.RoleId); role != nil {
		out["roleName"] = role.Name
		out["isSuperadmin"] = role.IsSuperadmin
		out["pending"] = false
	}
	if out["isSuperadmin"] != true {
		rows, err := a.perms.ListForRole(r.Context(), principal.RoleId)
		if err != nil {
			controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
			return
		}
		out["permissions"] = rows
	}
	controllers.SendResult(w, out, "succeed")
}

func (a *accessRbacApi) listRoles(w http.ResponseWriter, r *http.Request) {
	roles, err := a.roles.List(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, roles, "succeed")
}

func (a *accessRbacApi) createRole(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := decodeAccessRbacJSON(w, r, &body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	role, err := a.roles.Create(r.Context(), body.Name, body.Description)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, role, "succeed")
}

func (a *accessRbacApi) updateRole(w http.ResponseWriter, r *http.Request) {
	id := accessRbacPathInt(r, "id")
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := decodeAccessRbacJSON(w, r, &body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	if err := a.roles.Update(r.Context(), id, body.Name, body.Description); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"ok": true}, "succeed")
}

func (a *accessRbacApi) deleteRole(w http.ResponseWriter, r *http.Request) {
	if err := a.roles.Delete(r.Context(), accessRbacPathInt(r, "id")); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"ok": true}, "succeed")
}

func (a *accessRbacApi) listPermissions(w http.ResponseWriter, r *http.Request) {
	roleId, _ := strconv.ParseInt(r.URL.Query().Get("roleId"), 10, 64)
	if roleId <= 0 {
		controllers.SendError(w, controllers.ErrBadRequest, "roleId is required")
		return
	}
	rows, err := a.perms.ListForRole(r.Context(), roleId)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, rows, "succeed")
}

func (a *accessRbacApi) upsertPermission(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RoleId    int64  `json:"roleId"`
		Path      string `json:"path"`
		CanGet    bool   `json:"canGet"`
		CanPost   bool   `json:"canPost"`
		CanPut    bool   `json:"canPut"`
		CanDelete bool   `json:"canDelete"`
	}
	if err := decodeAccessRbacJSON(w, r, &body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	if body.RoleId <= 0 || body.Path == "" {
		controllers.SendError(w, controllers.ErrBadRequest, "roleId and path are required")
		return
	}
	row, err := a.perms.Set(r.Context(), entities.AccessRolePermission{
		RoleId: body.RoleId, Path: body.Path,
		CanGet: body.CanGet, CanPost: body.CanPost, CanPut: body.CanPut, CanDelete: body.CanDelete,
	})
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, row, "succeed")
}

func (a *accessRbacApi) deletePermission(w http.ResponseWriter, r *http.Request) {
	if err := a.perms.Delete(r.Context(), accessRbacPathInt(r, "id")); err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"ok": true}, "succeed")
}

func accessRbacPathInt(r *http.Request, key string) int64 {
	v, _ := strconv.ParseInt(mux.Vars(r)[key], 10, 64)
	return v
}

func decodeAccessRbacJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}
