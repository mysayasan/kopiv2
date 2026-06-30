package middlewares

import (
	"net/http"
	"sync"

	enumauth "github.com/mysayasan/kopiv2/domain/enums/auth"
	"github.com/mysayasan/kopiv2/domain/models"
	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

// PasswordChangeRequiredCode is the message the SPA keys on to route a must-change
// user to the change-password screen.
const PasswordChangeRequiredCode = "password_change_required"

// AccessSessionMidware enforces an app's own (accessrbac) authorization on its own
// endpoints: it rejects disabled/unknown users, blocks must-change users, lets a
// superadmin role through, and otherwise checks the role permission matrix. It runs
// AFTER AuthMidware (claims in context) and is mounted only on the app's module
// endpoints (not, e.g., a node tunnel governed by a different axis). The app
// supplies its user layer via sharedservices.AccessUserResolver.
type AccessSessionMidware struct {
	mu    sync.RWMutex
	users sharedservices.AccessUserResolver
	roles sharedservices.IAccessRoleService
	perms sharedservices.IAccessPermissionService
}

// NewAccessSessionMidware builds the middleware. users may be nil and set later via
// SetResolver — apphost constructs the middleware before the app builds its user
// store, and the app supplies the resolver during RegisterAppRoutes (it is read at
// request time, so it is ready before any request is served).
func NewAccessSessionMidware(
	users sharedservices.AccessUserResolver,
	roles sharedservices.IAccessRoleService,
	perms sharedservices.IAccessPermissionService,
) *AccessSessionMidware {
	return &AccessSessionMidware{users: users, roles: roles, perms: perms}
}

// SetResolver binds (or rebinds) the app's user resolver.
func (m *AccessSessionMidware) SetResolver(users sharedservices.AccessUserResolver) {
	m.mu.Lock()
	m.users = users
	m.mu.Unlock()
}

func (m *AccessSessionMidware) resolver() sharedservices.AccessUserResolver {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.users
}

// Middleware is the gorilla/mux middleware function.
func (m *AccessSessionMidware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, _ := r.Context().Value(enumauth.Claims).(*models.JwtCustomClaims)
		if claims == nil {
			controllers.SendError(w, controllers.ErrPermission, "not authenticated")
			return
		}
		users := m.resolver()
		if users == nil {
			// Fail closed: a protected route with no resolver bound is a misconfig.
			controllers.SendError(w, controllers.ErrPermission, "authorization is not configured")
			return
		}
		principal, err := users.ResolveAccessUser(r.Context(), claims.Id)
		if err != nil {
			controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
			return
		}
		if principal == nil || principal.Disabled {
			controllers.SendError(w, controllers.ErrPermission, "your session is no longer valid")
			return
		}
		if principal.MustChangePassword {
			controllers.SendError(w, controllers.ErrPermission, PasswordChangeRequiredCode)
			return
		}
		// The live DB role is authoritative — re-stamp it onto the request claims so an
		// admin's role change takes effect on the user's very next request (the token's
		// baked roleId is no longer trusted for authorization). Downstream handlers that
		// read claims.RoleId then see the current role without a re-login.
		claims.RoleId = principal.RoleId
		if role, _ := m.roles.GetById(r.Context(), principal.RoleId); role != nil && role.IsSuperadmin {
			next.ServeHTTP(w, r)
			return
		}
		allowed, err := m.perms.Authorize(r.Context(), principal.RoleId, r.URL.Path, r.Method)
		if err != nil {
			controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
			return
		}
		if !allowed {
			controllers.SendError(w, controllers.ErrLimitedAccess, "you do not have permission for this action")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireSuperadmin is a gorilla/mux middleware that admits only a superadmin session.
// Mount it on an admin API subrouter (after Middleware) to make the whole surface
// superadmin-only regardless of the permission matrix — so a broad GET/PUT grant (e.g.
// a viewer's `GET /api` wildcard) can never reach administrative data or actions.
func (m *AccessSessionMidware) RequireSuperadmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !m.IsSuperadmin(r) {
			controllers.SendError(w, controllers.ErrLimitedAccess, "superadmin access required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// CurrentPrincipal resolves the request's accessrbac principal via the bound user
// resolver (nil if unauthenticated or no resolver). Used by the "my permissions"
// endpoint so the SPA can compute menu visibility from the same matrix that gates
// the APIs.
func (m *AccessSessionMidware) CurrentPrincipal(r *http.Request) (*sharedservices.AccessPrincipal, error) {
	claims, _ := r.Context().Value(enumauth.Claims).(*models.JwtCustomClaims)
	if claims == nil {
		return nil, nil
	}
	users := m.resolver()
	if users == nil {
		return nil, nil
	}
	return users.ResolveAccessUser(r.Context(), claims.Id)
}

// IsSuperadmin reports whether the request's session role is a superadmin role.
// Used by management handlers that must self-gate to superadmin regardless of the
// permission matrix. It resolves the role LIVE from the user store (not the token's
// baked roleId) so a just-granted/revoked superadmin takes effect without a re-login.
func (m *AccessSessionMidware) IsSuperadmin(r *http.Request) bool {
	principal, err := m.CurrentPrincipal(r)
	if err != nil || principal == nil {
		return false
	}
	role, err := m.roles.GetById(r.Context(), principal.RoleId)
	return err == nil && role != nil && role.IsSuperadmin
}
