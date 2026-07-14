package apis

import (
	"context"
	"net/http"

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
	return sharedapis.NewLocalBasicAuth(sharedapis.LocalAuthConfig{
		AppName: "mymatasan",
		OnLockout: func(ctx context.Context, info sharedapis.LockoutInfo) {
			if notifier == nil {
				return
			}
			services.NotifyAuthLockout(ctx, notifier, services.AuthLockoutInfo{
				Username:    info.Username,
				IP:          info.IP,
				LockSeconds: info.LockSeconds,
			})
		},
	}, userService, guard)
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
func setLocalAuthCookie(w http.ResponseWriter, user *services.AuthenticatedUser) {
	sharedapis.SetLocalAuthCookie(w, localAuthConfig(), user)
}

// NewRequireRolePermission authorizes every request against the signed-in user's role.
func NewRequireRolePermission(
	roles sharedservices.IAccessRoleService,
	perms sharedservices.IAccessPermissionService,
) func(http.Handler) http.Handler {
	return sharedapis.NewRequireRolePermission(roles, perms)
}
