package apis

import (
	"net/http"
	"strings"

	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

// viewerWriteAllowSuffixes are the only mutating endpoints a non-admin (view-only)
// user may call: resolving/receiving a live stream, the login-time health probe,
// acknowledging an alert, marking a notification read, and changing their own
// password. Matched by path suffix so the numeric {id} segments don't matter.
var viewerWriteAllowSuffixes = []string{
	"/auth/change-password",
	"/live-view",
	"/webrtc/offer",
	"/health/refresh",
	"/ack",
	"/read",
}

// NewRequireAdminForWrites restricts non-admin users to read-only access plus the
// viewer allow-list above; every other mutating request is rejected with 403.
// Reads (GET/HEAD/OPTIONS) are always allowed — per-endpoint handlers can still
// add their own admin checks (e.g. the user-management routes).
//
// It must be registered AFTER NewLocalBasicAuth so the authenticated user is in
// the request context. Secure by default: any newly added mutating endpoint is
// admin-only until explicitly allow-listed here.
func NewRequireAdminForWrites() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := LocalUserFromContext(r.Context())
			if !ok {
				// The auth middleware should have populated this; fail closed.
				controllers.SendError(w, controllers.ErrLimitedAccess, "not authenticated")
				return
			}
			if user.IsAdmin || isReadOnlyMethod(r.Method) || isViewerWriteAllowed(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			controllers.SendError(w, controllers.ErrLimitedAccess, "admin access required")
		})
	}
}

func isReadOnlyMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	return false
}

func isViewerWriteAllowed(path string) bool {
	for _, suffix := range viewerWriteAllowSuffixes {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}
	return false
}
