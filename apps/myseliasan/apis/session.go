package apis

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myseliasan/services"
	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
	enumauth "github.com/mysayasan/kopiv2/domain/enums/auth"
	"github.com/mysayasan/kopiv2/domain/models"
	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

type sessionApi struct {
	auth  middlewares.AuthMidware
	users services.IControlUserService
	roles sharedservices.IAccessRoleService
	perms sharedservices.IAccessPermissionService
}

func NewSessionApi(router *mux.Router, auth middlewares.AuthMidware, users services.IControlUserService, roles sharedservices.IAccessRoleService, perms sharedservices.IAccessPermissionService) {
	handler := &sessionApi{auth: auth, users: users, roles: roles, perms: perms}
	group := router.PathPrefix("/session").Subrouter()
	group.Use(auth.Middleware)
	group.HandleFunc("/me", handler.me).Methods("GET")
}

// me returns the current session enriched with myseliasan's own authorization
// state — the role name, whether it is a superadmin, and whether the (local stock)
// account must change its password — so the SPA can route the login/handoff UX
// without an extra round-trip.
func (m *sessionApi) me(w http.ResponseWriter, r *http.Request) {
	claims, _ := r.Context().Value(enumauth.Claims).(*models.JwtCustomClaims)
	if claims == nil {
		controllers.SendError(w, controllers.ErrPermission, "not authenticated")
		return
	}

	out := map[string]any{
		"userId":   claims.Id,
		"email":    claims.Email,
		"name":     claims.Name,
		"roleId":   claims.RoleId,
		"issuer":   claims.Issuer,
		"audience": []string(claims.Audience),
	}

	// A user the control plane no longer recognizes (e.g. a retired stock account)
	// is treated as signed out so the SPA returns to the login screen.
	if m.users != nil {
		user, err := m.users.GetById(r.Context(), claims.Id)
		if err == nil && (user == nil || user.Disabled) {
			controllers.SendError(w, controllers.ErrPermission, "your session is no longer valid")
			return
		}
		if user != nil {
			out["mustChangePassword"] = user.MustChangePassword
			out["kind"] = user.Kind
			out["isStock"] = user.IsStock
		}
		// Drive the handoff banner: nag to disable the stock superadmin only once a
		// real superadmin is active (so the prompt appears exactly when it's safe).
		if stockActive, realActive, serr := m.users.SuperadminStatus(r.Context()); serr == nil {
			out["stockSuperadminActive"] = stockActive
			out["superadminHandoffPending"] = stockActive && realActive
		}
	}
	superadmin := false
	if m.roles != nil {
		if role, err := m.roles.GetById(r.Context(), claims.RoleId); err == nil && role != nil {
			out["roleName"] = role.Name
			out["isSuperadmin"] = role.IsSuperadmin
			superadmin = role.IsSuperadmin
		}
	}

	// The role's permission matrix lets the SPA gate the nav from the same rules that
	// gate the APIs (a tab is shown only if the role has GET on its path prefix).
	// Superadmin bypasses the matrix, so the SPA treats an empty list as a wildcard.
	out["permissions"] = []*sharedentities.AccessRolePermission{}
	if m.perms != nil && !superadmin {
		if rows, err := m.perms.ListForRole(r.Context(), claims.RoleId); err == nil {
			out["permissions"] = rows
		}
	}

	controllers.SendResult(w, out)
}
