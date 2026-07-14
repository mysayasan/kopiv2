package apis

import (
	"log"
	"net/http"

	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

// NewRequireRolePermission authorizes every request against the signed-in user's role.
//
// It replaces NewRequireAdminForWrites, which was:
//
//	if user.IsAdmin || isReadOnlyMethod(r.Method) || isViewerWriteAllowed(r.URL.Path)
//
// — one bool, every GET allowed to everybody, and a six-entry allow-list matched by
// strings.HasSuffix. Three things were wrong with that:
//
//   - There was NO read authorization at all. Any signed-in user could read every camera,
//     every recording and every alert snapshot in the system.
//   - "May watch cameras but may not delete recordings" was not expressible — the property
//     that makes an NVR evidentiary rather than a camera viewer.
//   - The allow-list matched by SUFFIX, so any future route ending in "/read" or "/ack"
//     would become silently writable by a non-admin. A booby trap for whoever added the
//     next endpoint.
//
// Now: a superadmin role passes; everyone else is decided by their role's permission
// matrix, which is deny-by-default. See services.Policy() for the catalog the built-in
// roles are seeded from.
//
// It must be registered AFTER NewLocalBasicAuth, so the authenticated user is in context.
func NewRequireRolePermission(
	roles sharedservices.IAccessRoleService,
	perms sharedservices.IAccessPermissionService,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := LocalUserFromContext(r.Context())
			if !ok {
				// The auth middleware should have populated this. Fail closed.
				controllers.SendError(w, controllers.ErrLimitedAccess, "not authenticated")
				return
			}

			// A superadmin bypasses the matrix. This is the one place "admin gets everything"
			// still lives, and it is now an explicit flag on a ROLE rather than a bool on a
			// user — see AuthenticatedUser.IsAdmin, which is derived from it.
			if user.IsAdmin {
				next.ServeHTTP(w, r)
				return
			}

			if user.RoleId <= 0 {
				// A user with no role has no permissions. Denying is the only safe reading of
				// an account an administrator has not finished setting up.
				controllers.SendError(w, controllers.ErrLimitedAccess, "your account has no role assigned — ask an administrator")
				return
			}

			allowed, err := perms.Authorize(r.Context(), user.RoleId, r.URL.Path, r.Method)
			if err != nil {
				// Fail CLOSED, and say so. An authorization check that cannot run is not a
				// reason to let the request through, and a silent deny is a support ticket.
				log.Printf("authorization: matrix lookup failed for role %d on %s %s: %v", user.RoleId, r.Method, r.URL.Path, err)
				controllers.SendError(w, controllers.ErrLimitedAccess, "authorization is unavailable")
				return
			}
			if !allowed {
				controllers.SendError(w, controllers.ErrLimitedAccess, "you do not have permission for this action")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
