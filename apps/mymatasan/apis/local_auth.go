package apis

import (
	"context"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
)

// The local-auth middleware, the failed-login guard and the RBAC gate now live in
// domain/shared/apis so every appliance app runs the same code. These bindings supply the
// parts that are mymatasan's: its app name (which names the session cookie and the Basic
// realm) and its notification stack (which surfaces a lockout as a security event).

type (
	// LoginGuard throttles failed sign-ins.
	LoginGuard = sharedapis.LoginGuard
	// LoginGuardConfig tunes the guard.
	LoginGuardConfig = sharedapis.LoginGuardConfig
)

// NewLoginGuard builds the failed-login lockout.
func NewLoginGuard(cfg LoginGuardConfig) *LoginGuard { return sharedapis.NewLoginGuard(cfg) }

// LocalUserFromContext returns the authenticated local mymatasan user.
func LocalUserFromContext(ctx context.Context) (*services.AuthenticatedUser, bool) {
	return sharedapis.LocalUserFromContext(ctx)
}

// NewLocalBasicAuth protects mymatasan's routes with DB-backed users (Basic + session
// cookie), the forced-password-change gate, and the failed-login lockout.
func NewLocalBasicAuth(userService services.ILocalUserService, guard *LoginGuard, notifier services.INotificationPublisher) func(http.Handler) http.Handler {
	return sharedapis.NewLocalBasicAuth(lockoutAuthConfig(notifier), userService, guard)
}

// NewLocalLoginApi registers mymatasan's PUBLIC sign-in and sign-out endpoints.
//
// This is what makes a page reload survivable. Before it existed, the SPA verified the
// credential by replaying HTTP Basic on every request and holding the password in the page's
// memory — so a refresh, which throws that memory away, was indistinguishable from signing
// out. Exchanging the credential once for the session cookie means the browser holds the
// session, not the page, and it also stops charging a bcrypt verification to every request.
func NewLocalLoginApi(router *mux.Router, userService services.ILocalUserService, guard *LoginGuard, notifier services.INotificationPublisher) {
	sharedapis.NewLocalLoginApi(router, lockoutAuthConfig(notifier), userService, guard)
}

// lockoutAuthConfig is mymatasan's binding plus the hook that surfaces a tripped lockout as a
// security notification. Shared by the middleware and the login endpoint so a lockout is
// reported the same way whichever of them trips it.
func lockoutAuthConfig(notifier services.INotificationPublisher) sharedapis.LocalAuthConfig {
	cfg := localAuthConfig()
	cfg.OnLockout = func(ctx context.Context, info sharedapis.LockoutInfo) {
		if notifier == nil {
			return
		}
		services.NotifyAuthLockout(ctx, notifier, services.AuthLockoutInfo{
			Username:    info.Username,
			IP:          info.IP,
			LockSeconds: info.LockSeconds,
		})
	}
	return cfg
}

// localAuthConfig is mymatasan's binding of the shared middleware: the app name that names
// the session cookie and the Basic realm.
func localAuthConfig() sharedapis.LocalAuthConfig {
	return sharedapis.LocalAuthConfig{AppName: "mymatasan"}
}

// withLocalUser injects a pre-authenticated principal (the control-channel dispatcher's
// synthetic actor) into a request context.
func withLocalUser(ctx context.Context, user *services.AuthenticatedUser) context.Context {
	return sharedapis.WithLocalUser(ctx, user)
}

// setLocalAuthCookie writes mymatasan's session cookie after a verified sign-in.
func setLocalAuthCookie(w http.ResponseWriter, r *http.Request, user *services.AuthenticatedUser) {
	sharedapis.SetLocalAuthCookie(w, r, localAuthConfig(), user)
}

// NewRequireRolePermission authorizes every request against the signed-in user's role.
func NewRequireRolePermission(
	roles sharedservices.IAccessRoleService,
	perms sharedservices.IAccessPermissionService,
) func(http.Handler) http.Handler {
	return sharedapis.NewRequireRolePermission(roles, perms)
}
